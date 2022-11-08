package ecs

import (
	"fmt"
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/pkg/errors"
	"golang.org/x/term"
)

// Helper function for reporting telemetry
func reportStep(stepName string) {
	telemetry.WorkflowStep("Add to ECS", stepName)
}

// A function which executes the next part of the workflow,
// and picks a next state or exits. Return "true" to continue
// so that the zero value exits.
type AddWorkflowState func(*AddWorkflow) (cont bool, err error)

type arn string

type AddWorkflow struct {
	currentState AddWorkflowState

	awsProfile string
	awsCred    *credentials.Credentials
	awsRegion  string

	session *session.Session

	ecsCluster    string
	ecsClusterArn arn

	ecsService string
	ecsTask    string
}

// Run the "add to ECS" workflow until we complete or get an error.
// Errors that are UsageErrors should be returned as-is; other
// errors should be wrapped to avoid showing usage.  (This is reversed
// from the other command conventions, but there are relatively few
// usage errors here.
func RunAddWorkflow() error {
	wf := &AddWorkflow{
		currentState: initState,
	}

	keepGoing := true
	var err error = nil
	for keepGoing && err == nil {
		keepGoing, err = wf.currentState(wf)
	}
	if err == nil {
		telemetry.Success("Add to ECS")
	} else if errors.Is(err, terminal.InterruptErr) {
		printer.Infof("Interrupted!\n")
		telemetry.WorkflowStep("Add to ECS", "User interrupted session")
		return nil
	} else if _, ok := err.(UsageError); ok {
		telemetry.Error("Add to ECS", err)
		return err
	} else {
		telemetry.Error("Add to ECS", err)
		return cmderr.AkitaErr{Err: err}
	}
	return err
}

// Initial state: check if running interactively, if so then start
// with collecting AWS profile.
func initState(wf *AddWorkflow) (keepGoing bool, err error) {
	reportStep("Start Add to ECS")

	// Check if running interactively.
	// TODO: I didn't see a way to do this from go-survey directly.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fillFromFlags(wf)
	}

	wf.currentState = getProfileState
	return true, nil
}

// Ask the user to specify a profile; "" is fine to use the default profile.
// TODO: it seems very difficult to present a list (which is what I was trying
// to do orginally) because the SDK doesn't provide an API to do that, and
// its config file parser is internal.
func getProfileState(wf *AddWorkflow) (keepGoing bool, err error) {
	reportStep("Get AWS Profile")

	if awsProfileFlag != "" {
		wf.awsProfile = awsProfileFlag
		if err = wf.checkCredentials(); err != nil {
			printer.Errorf("Could not find AWS credentials for profile %q. The error from the AWS library is shown below.\n")
			return false, errors.Wrap(err, "Error loading credentials")
		}

		wf.currentState = getRegionState
		return true, nil
	}

	// Use the existing value as the default in case we repeat this step
	err = survey.AskOne(
		&survey.Input{
			Message: "Which of your AWS profiles should Akita use to configure ECS?",
			Help:    "Enter the name of the AWS profile you use for configuring ECS, or leave blank to try the default profile. Akita needs this information to identify which AWS credentials to use.",
			Default: wf.awsProfile,
		},
		&wf.awsProfile,
	)
	if err != nil {
		return false, err
	}

	wf.awsCred = credentials.NewSharedCredentials(awsCredentialsFlag, wf.awsProfile)
	if err = wf.checkCredentials(); err != nil {
		printer.Errorf("Error from AWS library: %v\n", err)
		printer.Errorf("Could not find AWS credentials for profile %q, please try again or hit Ctrl+C to exit.\n", wf.awsProfile)
		wf.awsProfile = ""
		return true, nil
	}

	// TODO: I don't think there's even a way to show the profile name here?
	printer.Infof("Successfully loaded AWS credentials.\n")

	wf.currentState = getRegionState
	return true, nil
}

const findAllClusters = "find all clusters"

var publicAWSRegions = []string{
	"default",
	findAllClusters,
	"af-south-1",
	"ap-east-1",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-northeast-3",
	"ap-south-1",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-southeest-3",
	"ca-central-1",
	"eu-central-1",
	"eu-central-1",
	"eu-north-1",
	"eu-south-1",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"me-central-1",
	"me-south-1",
	"sa-east-1",
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
}

// Ask the user to select a region.
func getRegionState(wf *AddWorkflow) (keepGoing bool, err error) {
	reportStep("Get AWS Region")

	if awsRegionFlag != "" {
		wf.awsRegion = awsRegionFlag
		err = wf.createSession()
		if err != nil {
			printer.Errorf("Could not create a session to communicate with AWS. The error from the AWS library is shown below.\n")
			printer.Infof("Please contact support@akitasoftware.com for assistance.\n")
			return false, errors.Wrap(err, "Error creating session")
		}

		wf.currentState = getClusterState
		return true, nil
	}

	err = survey.AskOne(
		&survey.Select{
			Message: "In which AWS region is your ECS cluster?",
			Help:    "Select the AWS region where you run the ECS cluster with the task you want to modify. You can select 'find all clusters' and we will search for all ECS clusters you can access, or 'default' to use the one specified in your AWS configuration.",
			Options: publicAWSRegions,
		},
		&wf.awsRegion,
	)
	if err != nil {
		return false, err
	}

	if wf.awsRegion == "default" {
		err = wf.createSessionFromConfig()
		if err != nil {
			switch err.(type) {
			case session.SharedConfigProfileNotExistsError:
				printer.Errorf("The profile you selected does not exist in the configuration file.\n")
				printer.Infof("Please select a region from the list, or hit Ctrl+C to exit.\n")
			case session.SharedConfigLoadError:
				printer.Errorf("The Akita agent could not low an AWS configuration file: %v\n", err)
				printer.Infof("Please select a region from the list, or hit Ctrl+C to exit.\n")
			// TODO: what to do with SharedConfigAssumeRoleError?
			default:
				printer.Errorf("Error from AWS library: %v\n", err)
				printer.Errorf("Could not find a working default region. Please select one or hit Ctrl+C to exit.\n")
			}
			return true, nil
		}
		wf.currentState = getClusterState
		return true, nil
	}

	if wf.awsRegion == findAllClusters {
		wf.currentState = findClusterAndRegionState
		return true, nil
	}

	// TODO: what classes of error are possible here? It looks like only configuration problems.
	err = wf.createSession()
	if err != nil {
		printer.Errorf("Could not create a session to communicate with AWS region %q. The error from the AWS library is shown below.\n",
			wf.awsRegion)
		printer.Infof("Please contact support@akitasoftware.com, or try specifying the --region flag.\n")
		return false, errors.Wrap(err, "Error creating session")
	}

	wf.currentState = getClusterState
	return true, nil
}

// Search all regions for ECS clusters. The reason this is not the default
// is because it is very slow...
func findClusterAndRegionState(wf *AddWorkflow) (keepGoing bool, err error) {
	reportStep("Get ECS Cluster and Region")

	printer.Errorf("Unimplemented!\n")
	return false, nil
}

// Find all ECS clusters in the selected region.
func getClusterState(wf *AddWorkflow) (keepGoing bool, err error) {
	reportStep("Get ECS Cluster")

	if ecsClusterFlag != "" {
		// TODO: lookup arn
		return false, fmt.Errorf("Cluster flag is unimplemented")
	}

	clusters, listErr := wf.listECSClusters()
	if listErr != nil {
		// TODO: error handling, check permissions
		// TODO: offer a chance to try a different profile or cluster
		printer.Errorf("Could not list EC2 clusters: %v\n", listErr)
		return false, listErr
	}

	if len(clusters) == 0 {
		printer.Errorf("Could not find any ECS clusters in this region. Please select a different one or hit Ctrl+C to exit.\n")
		wf.currentState = getRegionState
		return true, nil
	}

	choices := make([]string, 0, len(clusters))
	for c, _ := range clusters {
		choices = append(choices, string(c))
	}

	// TODO: add "find all tasks" option?
	err = survey.AskOne(
		&survey.Select{
			Message: "In which cluster does your application run?",
			Help:    "Select ECS cluster with the task you want to modify.",
			Options: choices,
			Description: func(value string, _ int) string {
				return clusters[arn(value)]
			},
		},
		&wf.ecsClusterArn,
	)

	return false, nil
}

// Run non-interactively and attempt to fill in all information from
// command-line flags.
func fillFromFlags(wf *AddWorkflow) (keepGoing bool, err error) {
	reportStep("Fill ECS Info From Flags")

	// Try to use default profile, "", if none specified
	wf.awsCred = credentials.NewSharedCredentials(awsCredentialsFlag, awsProfileFlag)
	if err = wf.checkCredentials(); err != nil {
		return false, fmt.Errorf("Could not find AWS credentials for profile %q", awsProfileFlag)
	}

	// Default region is OK only if there there is a .config file with one.
	// TODO: how do we check this?
	if awsRegionFlag != "" {
		err = wf.createSession()
	} else {
		err = wf.createSessionFromConfig()
	}
	if err != nil {
		return false, errors.Wrapf(err, "Cannot establish a connection to AWS")
	}

	// The rest of these are easy because they're mandatory.
	if ecsClusterFlag == "" {
		return false, UsageErrorf("Must specify an ECS cluster to operate on.")
	}
	// TODO: look up cluster by name or ARN

	// TODO: could we support adding to a task but not restarting a service?
	if ecsServiceFlag == "" {
		return false, UsageErrorf("Must specify an ECS service to modify.")
	}
	// TODO: look up service by name or ARN

	if ecsTaskFlag == "" {
		return false, UsageErrorf("Must specify an ECS task to modify.")
	}
	// TODO: look up task by name or ARN

	return false, nil
}
