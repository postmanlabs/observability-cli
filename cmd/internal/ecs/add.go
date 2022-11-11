package ecs

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/go-utils/optionals"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/pkg/errors"
	"golang.org/x/term"
)

// Helper function for reporting telemetry
func reportStep(stepName string) {
	telemetry.WorkflowStep("Add to ECS", stepName)
}

// A function which executes the next part of the workflow,
// and picks a next state (Some) or exits (None), or signals an eror.
type AddWorkflowState func(*AddWorkflow) (next optionals.Optional[AddWorkflowState], err error)

// Helper functions for choosing the next state.
func awf_done() (optionals.Optional[AddWorkflowState], error) {
	// I do not understand why Go cannot infer this type.
	return optionals.None[AddWorkflowState](), nil
}

func awf_error(err error) (optionals.Optional[AddWorkflowState], error) {
	return optionals.None[AddWorkflowState](), err
}

func awf_next(f AddWorkflowState) (optionals.Optional[AddWorkflowState], error) {
	return optionals.Some[AddWorkflowState](f), nil
}

// Type for Amazon Resource Names, to distinguish from human-readable names
type arn string

type AddWorkflow struct {
	currentState AddWorkflowState
	ctx          context.Context

	awsProfile string
	awsConfig  aws.Config
	awsRegion  string
	awsRegions []string

	ecsClient *ecs.Client

	ecsCluster    string
	ecsClusterARN arn

	ecsService string
	ecsTask    string
}

// Run the "add to ECS" workflow until we complete or get an error.
// Errors that are UsageErrors should be returned as-is; other
// errors should be wrapped to avoid showing usage.  (This is reversed
// from the other command conventions, but there are relatively few
// usage errors here.)
func RunAddWorkflow() error {
	wf := &AddWorkflow{
		currentState: initState,
		ctx:          context.Background(),
		awsProfile:   "default",
	}

	nextState := optionals.Some[AddWorkflowState](initState)
	var err error = nil
	for nextState.IsSome() && err == nil {
		wf.currentState, _ = nextState.Get()
		nextState, err = wf.currentState(wf)
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

// State machine ASCII art:
//
//         init     ---> fillFromFlags --> modifyTask
//           |
//           V
//    --> getProfile
//    |      |
//    |      V
//    |-> getRegion  --> findClusterAndRegion
//    |      |                  |
//    |      V                  |
//    -- getCluster             |
//         ^  |                 |
//         |  V                 |
//       getTask   <------------
//         ^  |
//         |  V
//      getService
//           |
//           V
//        confirm
//           |
//           V
//       modifyTask
//           |
//           V
//   waitForModification
//           |
//           V
//     restartService
//           |
//           V
//     waitForRestart
//
//
// Backtracking occurs when there are permission errors, an empty result, or
// the user asks to go back a step.
//

// Initial state: check if running interactively, if so then start
// with collecting AWS profile.`
func initState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Start Add to ECS")

	// Check if running interactively.
	// TODO: I didn't see a way to do this from go-survey directly.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fillFromFlags(wf)
	}

	return awf_next(getProfileState)
}

// Ask the user to specify a profile; "" is fine to use the default profile.
// TODO: it seems very difficult to present a list (which is what I was trying
// to do orginally) because the SDK doesn't provide an API to do that, and
// its config file parser is internal.
func getProfileState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get AWS Profile")

	if awsProfileFlag != "" {
		wf.awsProfile = awsProfileFlag
		if err = wf.createConfig(); err != nil {
			if errors.Is(err, NoSuchProfileError) {
				printer.Errorf("The AWS credentials file does not have profile %q. The error from the AWS library is shown below.\n")
			}
			return awf_error(errors.Wrap(err, "Error loading AWS credentials"))
		}

		return awf_next(getRegionState)
	}

	err = survey.AskOne(
		&survey.Input{
			Message: "Which of your AWS profiles should Akita use to configure ECS?",
			Help:    "Enter the name of the AWS profile you use for configuring ECS, or leave blank to try the default profile. Akita needs this information to identify which AWS credentials to use.",
			// Use the existing value as the default in case we repeat this step
			Default: wf.awsProfile,
		},
		&wf.awsProfile,
	)
	if err != nil {
		return awf_error(err)
	}

	if err = wf.createConfig(); err != nil {
		if errors.Is(err, NoSuchProfileError) {
			printer.Errorf("Could not find AWS credentials for profile %q. Please try again or hit Ctrl+C to exit.\n", wf.awsProfile)
			wf.awsProfile = "default"
			return awf_next(getProfileState)
		}
		printer.Errorf("Could not load the AWS config file. The error from the AWS library is shown below. Please send this log message to support@akitasoftware.com for assistance.\n", err)
		return awf_error(errors.Wrapf(err, "Error loading AWS credentials"))
	}

	printer.Infof("Successfully loaded AWS credentials for profile %q\n", wf.awsProfile)

	return awf_next(getRegionState)
}

const findAllClustersOption = "Search all regions."
const goBackOption = "Return to previous choice."

// Ask the user to select a region.
func getRegionState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get AWS Region")

	if awsRegionFlag != "" {
		wf.awsRegion = awsRegionFlag
		wf.createClient(wf.awsRegion)
		return awf_next(getClusterState)
	}

	if wf.awsRegions == nil {
		wf.awsRegions = wf.listAWSRegions()
	}

	err = survey.AskOne(
		&survey.Select{
			Message: "In which AWS region is your ECS cluster?",
			Help:    "Select the AWS region where you run the ECS cluster with the task you want to modify. You can select 'find all clusters' and we will search for all ECS clusters you can access, or 'default' to use the one specified in your AWS configuration.",
			Options: append([]string{findAllClustersOption}, wf.awsRegions...),
			Default: wf.awsConfig.Region,
		},
		&wf.awsRegion,
	)
	if err != nil {
		return awf_error(err)
	}

	if wf.awsRegion == findAllClustersOption {
		return awf_next(findClusterAndRegionState)
	}

	wf.createClient(wf.awsRegion)
	return awf_next(getClusterState)
}

// Search all regions for ECS clusters. The reason this is not the default
// is because it is rather slow.
func findClusterAndRegionState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get ECS Cluster and Region")
	printer.Infof("Searching all regions for ECS clusters. This may take a minute to complete.\n")

	arnToRegion := make(map[arn]string, 0)
	arnToName := make(map[arn]string, 0)
	for _, region := range wf.awsRegions {
		wf.createClient(region)
		clusters, err := wf.listECSClusters()
		if err != nil {
			printer.Warningf("Skipping region %q, error: %v\n", region, err)
			continue
		}
		if len(clusters) > 0 {
			printer.Infof("Found %d clusters in region %q.\n", len(clusters), region)
			for a, n := range clusters {
				arnToRegion[a] = region
				arnToName[a] = n
			}
		}
	}

	choices := make([]string, 0, len(arnToName))
	for c, _ := range arnToName {
		choices = append(choices, string(c))
	}
	sort.Slice(choices, func(i, j int) bool {
		return choices[i] < choices[j]
	})

	var clusterAnswer string
	err = survey.AskOne(
		&survey.Select{
			Message: "In which cluster does your application run?",
			Help:    "Select ECS cluster with the task you want to modify.",
			Options: choices,
			Description: func(value string, _ int) string {
				name := arnToName[arn(value)]
				if name == "" {
					return ""
				}
				return name + " in " + arnToRegion[arn(value)]
			},
		},
		&clusterAnswer,
	)

	wf.ecsClusterARN = arn(clusterAnswer)
	wf.createClient(arnToRegion[wf.ecsClusterARN])
	wf.awsRegion = arnToRegion[wf.ecsClusterARN]
	wf.ecsCluster = arnToName[wf.ecsClusterARN]

	return awf_done()
}

// Find all ECS clusters in the selected region.
func getClusterState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get ECS Cluster")

	if ecsClusterFlag != "" {
		// TODO: lookup arn
		return awf_error(fmt.Errorf("Cluster flag is unimplemented"))
	}

	clusters, listErr := wf.listECSClusters()
	if listErr != nil {
		var uoe UnauthorizedOperationError
		if errors.As(listErr, &uoe) {
			// Permissions error, pick a different profile or region (or quit.)
			printer.Errorf("The provided credentials do not have permission to perform %s on ECS in region %s.\n",
				uoe.OperationName, wf.awsConfig.Region)
			printer.Infof("Please pick a different profile or region, or assign this permission in AWS IAM.\n")
			return awf_next(getProfileState)
		}
		printer.Errorf("Could not list ECS clusters: %v\n", listErr)
		return awf_error(errors.New("Error while listing ECS clusters; try using the --cluster flag instead."))
	}

	if len(clusters) == 0 {
		printer.Errorf("Could not find any ECS clusters in this region. Please select a different one or hit Ctrl+C to exit.\n")
		return awf_next(getRegionState)
	}

	printer.Infof("Found %d clusters in region %q.\n", len(clusters), wf.awsRegion)

	choices := make([]string, 0, len(clusters))
	for c, _ := range clusters {
		choices = append(choices, string(c))
	}
	choices = append(choices, goBackOption)

	var clusterAnswer string
	err = survey.AskOne(
		&survey.Select{
			Message: "In which cluster does your application run?",
			Help:    "Select ECS cluster with the task you want to modify.",
			Options: choices,
			Description: func(value string, _ int) string {
				return clusters[arn(value)]
			},
		},
		&clusterAnswer,
	)
	if err != nil {
		return awf_error(err)
	}

	if clusterAnswer == goBackOption {
		return awf_next(getRegionState)
	}
	wf.ecsClusterARN = arn(clusterAnswer)
	wf.ecsCluster = clusters[wf.ecsClusterARN]

	return awf_done()
}

// Run non-interactively and attempt to fill in all information from
// command-line flags.
func fillFromFlags(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Fill ECS Info From Flags")

	// Try to use default profile, "", if none specified
	if err = wf.createConfig(); err != nil {
		// TODO: understand error cases
		printer.Errorf("Error from AWS SDK: %v\n", err)
		return awf_error(fmt.Errorf("Could not find AWS credentials for profile %q", awsProfileFlag))
	}

	// Default region is OK only if there there is a .config file with one.
	// TODO: how do we check this?
	// it looks like "an AWS region is required" happens on the first call
	wf.createClientWithDefaultRegion()

	// The rest of these are easy because they're mandatory.
	if ecsClusterFlag == "" {
		return awf_error(UsageErrorf("Must specify an ECS cluster to operate on."))
	}
	// TODO: look up cluster by name or ARN

	// TODO: could we support adding to a task but not restarting a service?
	if ecsServiceFlag == "" {
		return awf_error(UsageErrorf("Must specify an ECS service to modify."))
	}
	// TODO: look up service by name or ARN

	if ecsTaskFlag == "" {
		return awf_error(UsageErrorf("Must specify an ECS task to modify."))
	}
	// TODO: look up task by name or ARN

	return awf_done()
}
