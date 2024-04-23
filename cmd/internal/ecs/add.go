package ecs

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/consts"
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

// Convert to the type needed for AWS APIs
func (a arn) Use() *string {
	s := string(a)
	return &s
}

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
	//
	// XXX The code enabled by this variable needs to be updated for Postman.
	//
	// The problem is that the container's assumed role needs permission to
	// read the configured secrets, which seems difficult to set up here.
	secretsEnabled bool
	akitaSecrets   secretState
}

const (
	// Tag to use for objects created by the Akita CLI
	akitaCreationTagKey       = "postman:created_by"
	akitaCreationTagValue     = "Postman Insights ECS integration"
	akitaModificationTagKey   = "postman:modified_by"
	akitaModificationTagValue = "Postman Insights ECS integration"

	// Separate AWS secrets for the key ID and key secret
	// TODO: make these configurable
	akitaSecretPrefix    = "postman/"
	defaultKeyIDName     = akitaSecretPrefix + "api_key_id"
	defaultKeySecretName = akitaSecretPrefix + "api_key_secret"

	// Akita CLI image locations
	akitaECRImage    = "public.ecr.aws/akitasoftware/akita-cli"
	akitaDockerImage = "akitasoftware/cli"

	// Postman Insights Agent image location
	postmanECRImage = "docker.postman.com/postman-insights-agent"
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
			Message: "Which of your AWS profiles should be used to configure ECS?",
			Help:    "Enter the name of the AWS profile you use for configuring ECS, or leave blank to try the default profile. This information is needed to identify which AWS credentials to use.",
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
		printer.Errorf("Could not load the AWS config file. The error from the AWS library is shown below. Please send this log message to %s for assistance.\n%v\n", consts.SupportEmail, err)
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
			Message: "Which task should be monitored?",
			Help:    "Select the ECS task definition to modify. We will add the Postman Insights Agent as a sidecar to the task.",
			Options: tasks,
		},
		&taskAnswer,
	)
	if err != nil {
		return awf_error(err)
	}

	if taskAnswer == goBackOption {
		return awf_next(getClusterState)
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
		return awf_error(errors.Errorf("Error while loading ECS task definition; please contact %s for assistance.", consts.SupportEmail))
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

	// Check that the postman-insights-agent is not already present
	for _, container := range output.ContainerDefinitions {
		image := aws.ToString(container.Image)
		if matchesImage(image, postmanECRImage) {
			printer.Errorf("The selected task definition already has the image %q; postman-insights-agent is already installed.\n", image)
			printer.Infof("Please select a different task definition, or hit Ctrl+C to exit.\n")
			return awf_next(getTaskState)
		}

		// Also detect the Akita CLI image, to avoid having two copies of the agent
		// running.
		if matchesImage(image, akitaECRImage) || matchesImage(image, akitaDockerImage) {
			printer.Errorf("The selected task definition already has the image %q, indicating that the Akita CLI is currently installed.\n", image)
			printer.Infof("The Akita CLI is no longer supported. Please uninstall it and try again.\n")
			printer.Infof("You can also select a different task definition, or hit Ctrl+C to exit.\n")
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
		service, err := wf.getServiceWithMatchingTask(arn(ecsServiceFlag))
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
			Help:    "Select the ECS service that will be updated with the modified task definition, so it can be monitored.",
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

// XXX Unused. Needs to be updated for Postman.
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
		// XXX This branch of code is disabled; needs to be updated for Postman.

		if !wf.akitaSecrets.idExists {
			printer.Infof("Create an AWS secret %q in region %q to hold your Akita API key ID.\n",
				defaultKeyIDName, wf.awsRegion)
		}
		if !wf.akitaSecrets.secretExists {
			printer.Infof("Create an AWS secret %q in region %q to hold your Akita API key secret.\n",
				defaultKeySecretName, wf.awsRegion)
		}
	}
	printer.Infof("Create a new version %d of task definition %q which includes the Postman Insights Agent as a sidecar.\n",
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
//
// XXX Unused. Needs to be updated for Postman.
//
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

// Create a new revision of the task definition which includes the postman-insights-agent container.
func modifyTaskState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Modify ECS Task Definition")

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

	const isEssential = false
	agentContainer := makeAgentContainerDefinition(
		optionals.Some(wf.awsRegion),
		optionals.Some(wf.ecsService),
		optionals.Some(wf.ecsTaskDefinitionFamily),
		isEssential,
	)

	// If running on EC2, a memory size is required if no task-level memory size is specified.
	// If running on Fargate, a task-level memory size is required, and the container-level
	// setting is optional.
	// We'll specify a memory reservation only when required, i.e., when the task-level memory
	// is absent.
	// TODO: come up with a better default value, 300MB is from internal dogfooding
	if input.Memory == nil {
		agentContainer.MemoryReservation = aws.Int32(300)
	}

	// TODO: cpu share is optional on EC2 but the default is "two CPU shares" which may be too large.
	// It is optional in Fargate

	input.ContainerDefinitions = append(input.ContainerDefinitions, agentContainer)

	output, err := wf.ecsClient.RegisterTaskDefinition(wf.ctx, input)
	if err != nil {
		if uoe, unauth := isUnauthorized(err); unauth {
			printer.Errorf("The provided credentials do not have permission to register an ECS task definition (operation %s).\n",
				uoe.OperationName)
			printer.Infof("Please start over with a different profile, or add this permission in IAM.\n")
			return awf_error(errors.New("Failed to update the ECS task definition due to insufficient permissions."))
		}
		printer.Errorf("Could not register an ECS task definition. The error from the AWS library is shown below. Please send this log message to %s for assistance.\n%v\n", consts.SupportEmail, err)
		return awf_error(errors.Wrap(err, "Error registering task definition"))
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

func makeAgentContainerDefinition(
	awsRegion optionals.Optional[string],
	ecsService optionals.Optional[string],
	ecsTaskDefinitionFamily optionals.Optional[string],
	essential bool,
) types.ContainerDefinition {
	pKey, pEnv := cfg.GetPostmanAPIKeyAndEnvironment()

	envs := []types.KeyValuePair{}
	addToEnv := func(name string, value string) {
		envs = append(envs, types.KeyValuePair{
			Name:  &name,
			Value: &value,
		})
	}

	// This is a no-op if valueOpt is None.
	addOptToEnv := func(name string, valueOpt optionals.Optional[string]) {
		value, exists := valueOpt.Get()
		if exists {
			addToEnv(name, value)
		}
	}

	if pEnv != "" {
		addToEnv("POSTMAN_ENV", pEnv)
	}

	addToEnv("POSTMAN_API_KEY", pKey)

	// Setting these optional environment variables will cause the traces to be
	// tagged.
	addOptToEnv("POSTMAN_AWS_REGION", awsRegion)
	addOptToEnv("POSTMAN_ECS_SERVICE", ecsService)
	addOptToEnv("POSTMAN_ECS_TASK", ecsTaskDefinitionFamily)

	var entryPoint []string

	if collectionId != "" {
		entryPoint = []string{
			"/postman-insights-agent",
			"apidump",
			"--collection",
			collectionId,
		}
	} else {
		entryPoint = []string{
			"/postman-insights-agent",
			"apidump",
			"--project",
			projectId,
		}
	}

	return types.ContainerDefinition{
		Name:        aws.String("postman-insights-agent"),
		EntryPoint:  entryPoint,
		Environment: envs,
		Essential:   aws.Bool(essential),
		Image:       aws.String(postmanECRImage),
	}
}

// Update a service with the newly created task definition
func updateServiceState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Update ECS Service")

	defer func() {
		if err != nil {
			printer.Warningf("The ECS service %q was not successfully updated, but a new task definition has been registered.\n",
				wf.ecsService)
			printer.Infof("You should visit the AWS console and either update the service to the task definition \"%s:%s\", or delete that version of the task definition.\n",
				wf.ecsTaskDefinitionFamily, wf.ecsTaskDefinition.Revision)
		}
	}()

	// The API documentation says that none of the other fields are required, and
	// that leaving them null will leave them unchanged.
	input := &ecs.UpdateServiceInput{
		Service:        wf.ecsServiceARN.Use(), // documentation says "name"?
		Cluster:        wf.ecsClusterARN.Use(),
		TaskDefinition: wf.ecsTaskDefinitionARN.Use(),
	}
	_, err = wf.ecsClient.UpdateService(wf.ctx, input)
	if err != nil {
		telemetry.Error("AWS ECS UpdateService", err)
		if uoe, unauth := isUnauthorized(err); unauth {
			printer.Errorf("The provided credentials do not have permission to update the ECS service %q (operation %s).\n",
				wf.ecsServiceARN, uoe.OperationName)
			return awf_error(errors.New("Failed to update the ECS service due to insufficient permissions."))
		}
		printer.Errorf("Could not update the ECS service %q. The error from the AWS library is shown below. Please send this log message to %s for assistance.\n%v\n", wf.ecsServiceARN, consts.SupportEmail, err)
		return awf_error(errors.Wrapf(err, "Error updating ECS service %q", wf.ecsServiceARN))
	}
	printer.Infof("Updated service %q with new version of task definition.\n", wf.ecsService)

	// Try to tag the service; this can't be done in the UpdateService call
	// but its failure is non-fatal.
	tagInput := &ecs.TagResourceInput{
		ResourceArn: wf.ecsServiceARN.Use(),
		Tags: []types.Tag{{
			Key:   aws.String(akitaModificationTagKey),
			Value: aws.String(akitaModificationTagValue),
		}},
	}
	_, tagErr := wf.ecsClient.TagResource(wf.ctx, tagInput)
	if tagErr == nil {
		printer.Infof("Tagged service %q with %q.\n", wf.ecsService, akitaModificationTagKey)
	} else {
		telemetry.Error("AWS ECS TagResource", tagErr)
		if uoe, unauth := isUnauthorized(tagErr); unauth {
			printer.Warningf("The provided credentials do not have permission to tag the ECS service %q (operation %s).\n",
				wf.ecsServiceARN, uoe.OperationName)
		} else {
			printer.Warningf("Failed to tag the ECS service: %v\n", tagErr)
		}
		printer.Infof("The service has been modified, but it will be harder to locate it to roll back changes.\n")
	}

	return awf_next(waitForRestartState)
}

func waitForRestartState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Wait for ECS Service")
	startTime := time.Now()
	endTime := startTime.Add(5 * time.Minute)

	failure := errors.New("Cannot determine whether service was successfully upgraded. Please check the AWS console.\n")

	// Check that there's a new deployment in the service
	deploymentID, deployment, err := wf.GetDeploymentMatchingTask(wf.ecsServiceARN)
	if err != nil {
		if errors.Is(err, noDeploymentFound) {
			printer.Errorf("Could not locate a deployment for the new task definition \"%s:%s\" (%s).\n",
				wf.ecsTaskDefinitionFamily,
				wf.ecsTaskDefinition.Revision,
				wf.ecsTaskDefinitionARN)
		} else {
			printer.Errorf("Error checking service state: %v\n", err)
		}
		return awf_error(failure)
	}
	printer.Infof("Found new deployment with ID %q; its state is %s.\n", deploymentID, deployment.RolloutState)

	if deployment.RolloutState == types.DeploymentRolloutStateInProgress {
		printer.Infof("Waiting for the deployment to reach COMPLETED.\n")
	}

	ticker := time.NewTicker(15 * time.Second)
	for deployment.RolloutState == types.DeploymentRolloutStateInProgress {
		select {
		case t := <-ticker.C:
			if t.After(endTime) {
				reportStep("EC2 Service Deployment Timeout")
				printer.Warningf("Giving up after five minutes.\n")
				if deployment.RunningCount > 0 {
					printer.Infof("Some tasks did start, indicating that the new task definition is OK.\n")
				}
				return awf_error(failure)
			}
			deployment, err = wf.GetDeploymentByID(wf.ecsServiceARN, deploymentID)
			if err != nil {
				printer.Warningf("Error checking service state: %v", err)
				continue
			}
			duration := time.Now().Sub(startTime)
			printer.Infof("%02d:%02d after starting deployment %q: %d failed tasks, %d running tasks, %d pending tasks, overall status %s/%s.\n",
				duration/time.Minute, (duration%time.Minute)/time.Second,
				deploymentID, deployment.FailedTasks, deployment.RunningCount, deployment.PendingCount,
				aws.ToString(deployment.Status), deployment.RolloutState)
		}
	}
	if deployment.RolloutState == types.DeploymentRolloutStateFailed {
		telemetry.Error("EC2 Deployment", errors.New(aws.ToString(deployment.RolloutStateReason)))
		printer.Errorf("Deployment of the new task definition failed. The reason given by AWS is: %q",
			aws.ToString(deployment.RolloutStateReason))
		// TODO: guide the user through this?
		printer.Infof("You can use the AWS web console to delete the task definition \"%s:%s\". The previous task definition should still be in use.\n",
			wf.ecsTaskDefinitionFamily, wf.ecsTaskDefinition.Revision)
		return awf_error(errors.Errorf("The modified task failed to deploy. Please contact %s for assistance.", consts.SupportEmail))
	}

	reportStep("ECS Service Updated")
	printer.Infof("Deployment successful! Please return to the Postman Insights project that you created.\n")
	return awf_done()
}
