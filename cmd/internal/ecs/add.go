package ecs

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/go-utils/optionals"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
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

	ecsTaskDefinitionFamily string
	ecsTaskDefinitionTags   []types.Tag
	ecsTaskDefinitionARN    arn
	ecsTaskDefinition       *types.TaskDefinition

	ecsService    string
	ecsServiceARN arn

	// TODO: provide a flag that re-enables use of secrets
	// The problem is that the container's assumed role needs permission to
	// read the configured secrets, which seems difficult to set up here.
	secretsEnabled bool
	akitaSecrets   secretState
}

const (
	// Tag to use for objects created by the Akita CLI
	akitaCreationTagKey       = "akita.software:created_by"
	akitaCreationTagValue     = "Akita Software ECS integration"
	akitaModificationTagKey   = "akita.software:modified_by"
	akitaModificationTagValue = "Akita Software ECS integration"

	// Separate AWS secrets for the key ID and key secret
	// TODO: make these configurable
	akitaSecretPrefix    = "akita.software/"
	defaultKeyIDName     = akitaSecretPrefix + "api_key_id"
	defaultKeySecretName = akitaSecretPrefix + "api_key_secret"

	// Akita agent image locations
	akitaECRImage    = "public.ecr.aws/akitasoftware/akita-cli"
	akitaDockerImage = "akitasoftware/cli"
)

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
//    |    ^  |                 |
//    |    |  V                 |
//    |- getTask   <------------
//    |    ^  |
//    |    |  V
//    |-getService
//           |
//           |         getSecret
//           |         [disabled]
//           |
//           V
//        confirm
//           |
//           |
//           |         addSecret
//           |         [disabled]
//           V
//       modifyTask
//           |
//           V
//      updateService
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
			Help:    "Select the AWS region where you run the ECS cluster with the task you want to modify. You can select 'Search all regions' and we will search for all ECS clusters you can access.",
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

	if len(arnToRegion) == 0 {
		printer.Errorf("Could not find any ECS clusters in any region. Please select a different profile or hit Ctrl+C to exit.\n")
		return awf_next(getProfileState)
	}

	choices := make([]string, 0, len(arnToName))
	for c, _ := range arnToName {
		choices = append(choices, string(c))
	}
	sort.Strings(choices)

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
	if err != nil {
		return awf_error(err)
	}

	wf.ecsClusterARN = arn(clusterAnswer)
	wf.ecsCluster = arnToName[wf.ecsClusterARN]
	wf.awsRegion = arnToRegion[wf.ecsClusterARN]
	wf.createClient(wf.awsRegion)

	return awf_next(getTaskState)
}

func (wf *AddWorkflow) loadClusterFromFlag() (nextState optionals.Optional[AddWorkflowState], err error) {
	if strings.HasPrefix(ecsClusterFlag, "arn:") {
		clusterName, err := wf.getClusterName(arn(ecsClusterFlag))
		if err != nil {
			if errors.Is(err, NoSuchClusterError) {
				return awf_error(fmt.Errorf("Could not find cluster with ARN %q in region %s", ecsClusterFlag, wf.awsRegion))
			}
			return awf_error(errors.Wrap(err, "Error accessing cluster"))
		}
		wf.ecsClusterARN = arn(ecsClusterFlag)
		wf.ecsCluster = clusterName
		return awf_next(getTaskState)
	} else {
		clusters, listErr := wf.listECSClusters()
		if listErr != nil {
			return awf_error(errors.Wrap(err, "Error listing clusters"))
		}
		for a, name := range clusters {
			if name == ecsClusterFlag {
				printer.Infof("Found cluster %q matching name %q.\n", a, name)
				wf.ecsClusterARN = a
				wf.ecsCluster = name
				return awf_next(getTaskState)
			}
		}
		return awf_error(fmt.Errorf("No cluster found with name %q", ecsClusterFlag))
	}
}

// Find all ECS clusters in the selected region.
func getClusterState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get ECS Cluster")

	if ecsClusterFlag != "" {
		return wf.loadClusterFromFlag()
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
	sort.Strings(choices)
	choices = append(choices, goBackOption)

	var clusterAnswer string
	err = survey.AskOne(
		&survey.Select{
			Message: "In which cluster does your application run?",
			Help:    "Select ECS cluster with the task definition you want to modify.",
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

	return awf_next(getTaskState)
}

func (wf *AddWorkflow) loadTaskFromFlag() (nextState optionals.Optional[AddWorkflowState], err error) {
	// This call will work even if the flag is an ARN or a family:revision string.
	// TODO: should we check for those? Or just allow it?
	output, tags, describeErr := wf.getLatestECSTaskDefinition(ecsTaskDefinitionFlag)
	if describeErr != nil {
		var uoe UnauthorizedOperationError
		if errors.As(describeErr, &uoe) {
			printer.Errorf("The provided credentials do not have permission to perform %s on the task definition %q.\n",
				uoe.OperationName, wf.ecsTaskDefinitionFamily)
		}
		return awf_error(errors.Wrap(describeErr, "Error loading task definition"))
	}
	wf.ecsTaskDefinition = output
	wf.ecsTaskDefinitionFamily = aws.ToString(output.Family)
	wf.ecsTaskDefinitionARN = arn(aws.ToString(output.TaskDefinitionArn))
	wf.ecsTaskDefinitionTags = tags
	return awf_next(getServiceState)
}

// Find all task definitions. These are not technically tied to a cluster, but they are tied to a region.
// We could move this to immediately after picking the region, but it has to be after the combined
// region/cluster choice, so it's somewhat more consistent to do it here?
func getTaskState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get ECS Task Definition")

	if ecsTaskDefinitionFlag != "" {
		return wf.loadTaskFromFlag()
	}

	tasks, listErr := wf.listECSTaskDefinitionFamilies()
	if listErr != nil {
		var uoe UnauthorizedOperationError
		if errors.As(listErr, &uoe) {
			// Permissions error, go all the way back to profile selection.
			printer.Errorf("The provided credentials do not have permission to perform %s in the region %s.\n",
				uoe.OperationName, wf.awsRegion)
			printer.Infof("Please choose a different profile or region, or assign this permission in AWS IAM.\n")
			return awf_next(getProfileState)
		}
		printer.Errorf("Could not list ECS task definitions: %v\n", listErr)
		return awf_error(errors.New("Error while listing ECS task definitions; try using the --task flag instead."))
	}

	if len(tasks) == 0 {
		printer.Errorf("Could not find any ECS tasks in this cluster. Please select a different one or hit Ctrl+C to exit.\n")
		return awf_next(getClusterState)
	}

	printer.Infof("Found %d task definitions.\n", len(tasks))

	sort.Strings(tasks)
	tasks = append(tasks, goBackOption)

	var taskAnswer string
	err = survey.AskOne(
		&survey.Select{
			Message: "Which task should Akita monitor?",
			Help:    "Select the ECS task definition to modify. We will add the Akita agent as a sidecar to the task.",
			Options: tasks,
		},
		&taskAnswer,
	)
	if err != nil {
		return awf_error(err)
	}

	if taskAnswer == goBackOption {
		return awf_next(getRegionState)
	}
	wf.ecsTaskDefinitionFamily = taskAnswer

	// Load the task definition (if we don't have permission, retry.)
	output, tags, describeErr := wf.getLatestECSTaskDefinition(wf.ecsTaskDefinitionFamily)
	if describeErr != nil {
		var uoe UnauthorizedOperationError
		if errors.As(describeErr, &uoe) {
			printer.Errorf("The provided credentials do not have permission to perform %s on the task definition %q.\n",
				uoe.OperationName, wf.ecsTaskDefinitionFamily)
			printer.Infof("Please choose a different task definition, or assign this permission in AWS IAM.\n")
			return awf_next(getTaskState)
		}
		printer.Errorf("Could not load ECS task definition: %v\n", describeErr)
		return awf_error(errors.New("Error while loading ECS task definition; please contact support@akitasoftware.com for assistance."))
	}

	wf.ecsTaskDefinition = output
	wf.ecsTaskDefinitionARN = arn(aws.ToString(output.TaskDefinitionArn))
	wf.ecsTaskDefinitionTags = tags

	// Check that the task definition was not already modified.
	for _, tag := range tags {
		switch aws.ToString(tag.Key) {
		case akitaCreationTagKey, akitaModificationTagKey:
			printer.Errorf("The selected task definition already has the tag \"%s=%s\", indicating it was previously modified.\n",
				aws.ToString(tag.Key), aws.ToString(tag.Value))
			printer.Infof("Please select a different task definition, or remove this tag.\n")
			return awf_next(getTaskState)
		}
	}

	// Check that the Akita CLI is not already present
	for _, container := range output.ContainerDefinitions {
		image := aws.ToString(container.Image)
		if matchesImage(image, akitaECRImage) || matchesImage(image, akitaDockerImage) {
			printer.Errorf("The selected task definition already has the image %q; Akita is already installed.\n", image)
			printer.Infof("Please select a different task definition, or hit Ctrl+C to exit.\n")
			return awf_next(getTaskState)
		}
	}

	return awf_next(getServiceState)
}

func matchesImage(imageName, baseName string) bool {
	imageTokens := strings.Split(imageName, ":")
	return imageTokens[0] == baseName
}

func (wf *AddWorkflow) loadServiceFromFlag() (nextState optionals.Optional[AddWorkflowState], err error) {
	if strings.HasPrefix(ecsServiceFlag, "arn:") {
		service, err := wf.getService(arn(ecsServiceFlag))
		if err != nil {
			return awf_error(errors.Wrap(err, "Error accessing service"))
		}
		wf.ecsService = aws.ToString(service.ServiceName)
		wf.ecsServiceARN = arn(ecsServiceFlag)
		return awf_next(confirmState)
	}

	services, listErr := wf.listECSServices()
	if listErr != nil {
		return awf_error(errors.Wrap(err, "Error listing services"))
	}
	for a, name := range services {
		if name == ecsServiceFlag {
			printer.Infof("Found service %q matching name %q.\n", a, name)
			wf.ecsServiceARN = a
			wf.ecsService = name
			return awf_next(confirmState)
		}
	}
	return awf_error(fmt.Errorf("No service found with name %q that uses task definition %q", ecsServiceFlag, wf.ecsTaskDefinitionFamily))
}

// Find all services in the cluster that match the task definition.
func getServiceState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get ECS Service")

	if ecsServiceFlag != "" {
		return wf.loadServiceFromFlag()
	}

	services, listErr := wf.listECSServices()
	if listErr != nil {
		var uoe UnauthorizedOperationError
		if errors.As(listErr, &uoe) {
			printer.Errorf("The provided credentials do not have permission to perform %s in the cluster %q.\n",
				uoe.OperationName, wf.ecsCluster)
			printer.Infof("Please choose a different cluster, or assign this permission in AWS IAM.\n")
			return awf_next(getClusterState)
		}
		printer.Errorf("Could not list ECS services: %v\n", listErr)
		return awf_error(errors.New("Error while listing ECS services; try using the --service flag instead."))
	}

	if len(services) == 0 {
		printer.Errorf("Could not find any ECS services in cluster %q that use task definition %q. Please select a different task definition or hit Ctrl+C to exit.\n",
			wf.ecsCluster, wf.ecsTaskDefinitionFamily)
		return awf_next(getTaskState)
	}

	printer.Infof("Found %d services in cluster %q with task definition %q.\n", len(services), wf.ecsCluster, wf.ecsTaskDefinitionFamily)

	choices := make([]string, 0, len(services))
	for c, _ := range services {
		choices = append(choices, string(c))
	}
	sort.Strings(choices)
	choices = append(choices, goBackOption)
	// TODO: allow skipping this step?

	var serviceAnswer string
	err = survey.AskOne(
		&survey.Select{
			Message: "Which service should be updated to use the modified task definition?",
			Help:    "Select the ECS service that will be updated with the modified task definition, so it can be monitored by Akita.",
			Options: choices,
			Description: func(value string, _ int) string {
				return services[arn(value)]
			},
		},
		&serviceAnswer,
	)
	if err != nil {
		return awf_error(err)
	}
	if serviceAnswer == goBackOption {
		return awf_next(getTaskState)
	}
	wf.ecsServiceARN = arn(serviceAnswer)
	wf.ecsService = services[wf.ecsServiceARN]

	return awf_next(confirmState)
}

func getSecretState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get Akita Secrets")

	state, err := wf.checkAkitaSecrets()
	if err != nil {
		var uoe UnauthorizedOperationError
		if errors.As(err, &uoe) {
			printer.Errorf("The provided credentials do not have permission to list AWS secrets (operation %s).\n",
				uoe.OperationName)
			printer.Infof("Please choose a different profile, or assign this permission in AWS IAM.\n")
			return awf_next(getProfileState)
		}
		return awf_error(errors.Wrapf(err, "Error while checking for the Akita credentials secret in AWS"))
	}

	// TODO: later, we could allow the user to specify a name or ask whether they want to update
	// the existing secret with new credentials.

	wf.akitaSecrets = state
	return awf_next(confirmState)
}

func (wf *AddWorkflow) showPlannedChanges() {
	printer.Infof("--- Planned changes ---\n")
	if wf.secretsEnabled {
		if !wf.akitaSecrets.idExists {
			printer.Infof("Create an AWS secret %q in region %q to hold your Akita API key ID.\n",
				defaultKeyIDName, wf.awsRegion)
		}
		if !wf.akitaSecrets.secretExists {
			printer.Infof("Create an AWS secret %q in region %q to hold your Akita API key secret.\n",
				defaultKeySecretName, wf.awsRegion)
		}
	}
	printer.Infof("Create a new version %d of task definition %q which includes the Akita agent as a sidecar.\n",
		wf.ecsTaskDefinition.Revision+1, wf.ecsTaskDefinitionFamily)
	printer.Infof("Update service %q in cluster %q to the new task definition.\n",
		wf.ecsService, wf.ecsCluster)
}

func confirmState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Confirm Changes")

	wf.showPlannedChanges()

	if dryRunFlag {
		printer.Infof("Not making any changes due to --dry-run flag.\n")
		reportStep("Dry Run Completed")
		return awf_done()
	}

	proceed := false
	prompt := &survey.Confirm{
		Message: "Proceed with the changes?",
	}
	survey.AskOne(prompt, &proceed)

	if !proceed {
		// TODO: let the user back up instead?
		// (I realized one problem with this is if the last step had a flag, they are just
		// stucke anyway.)
		printer.Infof("No changes applied; exiting.\n")
		reportStep("Changes Rejected")
		return awf_done()
	}

	return awf_next(modifyTaskState)
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
	_, err = wf.loadClusterFromFlag()
	if err != nil {
		return awf_error(err)
	}

	if ecsTaskDefinitionFlag == "" {
		return awf_error(UsageErrorf("Must specify an ECS task definition to modify."))
	}
	_, err = wf.loadTaskFromFlag()
	if err != nil {
		return awf_error(err)
	}

	// TODO: could we support adding to a task but not restarting a service?
	if ecsServiceFlag == "" {
		return awf_error(UsageErrorf("Must specify an ECS service to modify."))
	}
	_, err = wf.loadServiceFromFlag()
	if err != nil {
		return awf_error(err)
	}

	wf.akitaSecrets, err = wf.checkAkitaSecrets()
	if err != nil {
		return awf_error(err)
	}

	wf.showPlannedChanges()

	if dryRunFlag {
		printer.Infof("Not making any changes due to -dry-run flag.\n")
		return awf_done()
	}

	return awf_next(modifyTaskState)
}

// Add the missing secrets.
// (TODO: if one is absent but the other is present we really ought to update both, but that's
// a separate AWS call so the logic is more complicated.)
func addSecretState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Add AWS Secret")

	key, secret := cfg.GetAPIKeyAndSecret()
	var secretErr error
	var secretArn arn
	if !wf.akitaSecrets.idExists {
		secretArn, secretErr = wf.createAkitaSecret(
			defaultKeyIDName,
			key,
			"Akita Software API key identifier, created for use in ECS.",
		)
		if secretErr == nil {
			printer.Infof("Created AWS secret %q\n", defaultKeyIDName)
			wf.akitaSecrets.idARN = secretArn
		}
	}

	if secretErr == nil && !wf.akitaSecrets.secretExists {
		secretArn, secretErr = wf.createAkitaSecret(
			defaultKeySecretName,
			secret,
			"Akita Software API key secret, created for use in ECS.",
		)
		if secretErr == nil {
			printer.Infof("Created AWS secret %q\n", defaultKeySecretName)
			wf.akitaSecrets.secretARN = secretArn
		}
	}

	if secretErr != nil {
		var uoe UnauthorizedOperationError
		if errors.As(secretErr, &uoe) {
			printer.Errorf("The provided credentials do not have permission to create or tag an AWS secret (operation %s).\n",
				uoe.OperationName)
			printer.Infof("Please start over with a different profile, or add this permission in IAM.\n")
			return awf_error(errors.New("Failed to add an AWS secret due to insufficient permissions."))
		}
		return awf_error(errors.Wrapf(secretErr, "Failed to add an AWS secret"))
	}

	return awf_next(modifyTaskState)
}

// Create a new revision of the task definition which includes the Akita container.
func modifyTaskState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	// Copy over all the state from the existing one.
	// IDK why they didn't reuse the types.ContainerDefinition type.
	prev := wf.ecsTaskDefinition
	input := &ecs.RegisterTaskDefinitionInput{
		ContainerDefinitions:    prev.ContainerDefinitions,
		Family:                  prev.Family,
		Cpu:                     prev.Cpu,
		EphemeralStorage:        prev.EphemeralStorage,
		ExecutionRoleArn:        prev.ExecutionRoleArn,
		InferenceAccelerators:   prev.InferenceAccelerators,
		IpcMode:                 prev.IpcMode,
		Memory:                  prev.Memory,
		NetworkMode:             prev.NetworkMode,
		PidMode:                 prev.PidMode,
		PlacementConstraints:    prev.PlacementConstraints,
		ProxyConfiguration:      prev.ProxyConfiguration,
		RequiresCompatibilities: prev.RequiresCompatibilities,
		RuntimePlatform:         prev.RuntimePlatform,
		Tags:                    wf.ecsTaskDefinitionTags,
		TaskRoleArn:             prev.TaskRoleArn,
		Volumes:                 prev.Volumes,
	}

	input.Tags = append(input.Tags, types.Tag{
		Key:   aws.String(akitaCreationTagKey),
		Value: aws.String(akitaCreationTagValue),
	})

	apiKey, apiSecret := cfg.GetAPIKeyAndSecret()
	input.ContainerDefinitions = append(input.ContainerDefinitions, types.ContainerDefinition{
		Name: aws.String("akita-agent"),
		// TODO: Cpu and Memory should be omitted for Fargate; they take their default values for EC2 if omitted.
		// For now we can leave the defaults in place, but they might be a bit large for EC2.
		EntryPoint: []string{"/akita", "apidump", "--project", projectFlag},
		Environment: []types.KeyValuePair{
			{Name: aws.String("AKITA_API_KEY_ID"), Value: &apiKey},
			{Name: aws.String("AKITA_API_KEY_SECRET"), Value: &apiSecret},
			
			// Setting these environment variables will cause the traces to be tagged.
			{Name: aws.String("AKITA_AWS_REGION"), Value: &wf.awsRegion},
			{Name: aws.String("AKITA_ECS_SERVICE"), Value: &wf.ecsService},
			{Name: aws.String("AKITA_ECS_TASK"), Value: &wf.ecsTaskDefinitionFamily},
		},
		Essential: aws.Bool(false),
		Image:     aws.String(akitaECRImage),
		Secrets:   []types.Secret{},
	})

	output, err := wf.ecsClient.RegisterTaskDefinition(wf.ctx, input)
	if err != nil {
		if uoe, unauth := isUnauthorized(err); unauth {
			printer.Errorf("The provided credentials do not have permission to register an ECS task definition (operation %s).\n",
				uoe.OperationName)
			printer.Infof("Please start over with a different profile, or add this permission in IAM.\n")
			return awf_error(errors.New("Failed to update the ECS task definition due to insufficient permissions."))
		}
	}
	printer.Infof("Registered task definition %q revision %d.\n",
		aws.ToString(output.TaskDefinition.Family),
		output.TaskDefinition.Revision)

	// Update the workflow state with the new task definition
	wf.ecsTaskDefinition = output.TaskDefinition
	wf.ecsTaskDefinitionARN = arn(aws.ToString(output.TaskDefinition.TaskDefinitionArn))
	wf.ecsTaskDefinitionTags = output.Tags

	return awf_next(updateServiceState)
}

func updateServiceState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	return awf_error(errors.New("unimplemented!"))
}
