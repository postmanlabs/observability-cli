package ecs

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	akitaCreationTagKey       = "postman:created_by"
	akitaCreationTagValue     = "Postman Live Insights ECS integration"
	akitaModificationTagKey   = "postman:modified_by"
	akitaModificationTagValue = "Postman Live Insights ECS integration"

	// Separate AWS secrets for the key ID and key secret
	// TODO: make these configurable
	akitaSecretPrefix    = "postman/"
	defaultKeyIDName     = akitaSecretPrefix + "api_key_id"
	defaultKeySecretName = akitaSecretPrefix + "api_key_secret"

	// Postman Live Collections Agent image locations
	akitaECRImage    = "public.ecr.aws/akitasoftware/akita-cli"
	akitaDockerImage = "akitasoftware/cli"
	postmanECRImage  = "docker.postman.com/postman-lc-agent"
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
//         init
//           |
//           V
//    --> getProfile
//    |      |
//    |      V
//    |-> getRegion
//    |      |
//    |      V
//    -- getCluster
//    |    ^  |
//    |    |  V
//    |- getService
//    |    ^  |
//    |    |  V
//    |-getTask
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

// Initial state
func initState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Start Add to ECS")

	return awf_next(getProfileState)
}

// Load credentials for awsProfile, if not specified "default" profile is used
func getProfileState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get AWS Profile")

	wf.awsProfile = awsProfileFlag
	if err = wf.createConfig(); err != nil {
		if errors.Is(err, NoSuchProfileError) {
			printer.Errorf("The AWS credentials file does not have profile %q. The error from the AWS library is shown below.\n")
		}
		return awf_error(errors.Wrap(err, "Error loading AWS credentials"))
	}

	printer.Infof("Successfully loaded AWS credentials for profile %q\n", wf.awsProfile)

	return awf_next(getRegionState)
}

// Ask the user to select a region.
func getRegionState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get AWS Region")

	wf.awsRegion = awsRegionFlag
	wf.createClient(wf.awsRegion)
	return awf_next(getClusterState)
}

// Get cluster state
func getClusterState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get ECS Cluster")
	clusterName, err := wf.getClusterName(arn(ecsClusterFlag))
	if err != nil {
		if errors.Is(err, NoSuchClusterError) {
			return awf_error(fmt.Errorf("could not find cluster with ARN %q in region %s", ecsClusterFlag, wf.awsRegion))
		}
		return awf_error(errors.Wrap(err, "Error accessing cluster"))
	}
	wf.ecsClusterARN = arn(ecsClusterFlag)
	wf.ecsCluster = clusterName
	printer.Infof("Successfully fetched ECS cluster with ARN %q\n", wf.ecsClusterARN)
	return awf_next(getServiceState)
}

// Find ECS service using the ARN.
func getServiceState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get ECS Service")

	service, err := wf.getService(arn(ecsServiceFlag))
	if err != nil {
		return awf_error(errors.Wrap(err, "Error accessing service"))
	}
	wf.ecsService = aws.ToString(service.ServiceName)
	wf.ecsServiceARN = arn(ecsServiceFlag)
	wf.ecsTaskDefinitionARN = arn(*service.TaskDefinition)
	printer.Infof("Successfully fetched ECS service with ARN %q\n", wf.ecsServiceARN)
	return awf_next(getTaskState)
}

// Describe task definition, using ecsTaskDefinitionArn fetched from ECS describeService
// in previous step
func getTaskState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Get ECS Task Definition")

	output, tags, describeErr := wf.getECSTaskDefinition(arn(wf.ecsTaskDefinitionARN))
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
	// Check that the task definition was not already modified.
	for _, tag := range tags {
		switch aws.ToString(tag.Key) {
		case akitaCreationTagKey, akitaModificationTagKey:
			printer.Errorf("The selected task definition already has the tag \"%s=%s\", indicating it was previously modified.\n",
				aws.ToString(tag.Key), aws.ToString(tag.Value))
			printer.Infof("Please select a different task definition, or remove this tag.\n")
			return awf_next(confirmState)
		}
	}

	// Check that the postman-lc-agent is not already present
	for _, container := range output.ContainerDefinitions {
		image := aws.ToString(container.Image)
		if matchesImage(image, postmanECRImage) || matchesImage(image, akitaECRImage) || matchesImage(image, akitaDockerImage) {
			printer.Errorf("The selected task definition already has the image %q; postman-lc-agent is already installed.\n", image)
			printer.Infof("Please provide a different service or delete the task definition\n %q", wf.ecsTaskDefinitionARN)
			return awf_done()
		}
	}

	printer.Infof("Successfully fetched ECS task with ARN %q\n", wf.ecsTaskDefinitionARN)
	return awf_next(confirmState)
}

func matchesImage(imageName, baseName string) bool {
	imageTokens := strings.Split(imageName, ":")
	return imageTokens[0] == baseName
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
	printer.Infof("Create a new version of task definition %q which includes the Postman Live Collections Agent as a sidecar.\n",
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

// Create a new revision of the task definition which includes the postman-lc-agent container.
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

	pKey, pEnv := cfg.GetPostmanAPIKeyAndEnvironment()
	envs := []types.KeyValuePair{}
	if pEnv != "" {
		envs = append(envs, []types.KeyValuePair{
			{Name: aws.String("POSTMAN_ENV"), Value: &pEnv},
		}...)
	}
	input.ContainerDefinitions = append(input.ContainerDefinitions, types.ContainerDefinition{
		Name: aws.String("postman-lc-agent"),
		// TODO: Cpu and Memory should be omitted for Fargate; they take their default values for EC2 if omitted.
		// For now we can leave the defaults in place, but they might be a bit large for EC2.
		EntryPoint: []string{"/postman-lc-agent", "apidump", "--collection", collectionId},
		Environment: append(envs, []types.KeyValuePair{
			{Name: aws.String("POSTMAN_API_KEY"), Value: &pKey},
			// Setting these environment variables will cause the traces to be tagged.
			{Name: aws.String("AKITA_AWS_REGION"), Value: &wf.awsRegion},
			{Name: aws.String("AKITA_ECS_SERVICE"), Value: &wf.ecsService},
			{Name: aws.String("AKITA_ECS_TASK"), Value: &wf.ecsTaskDefinitionFamily},
		}...),
		Essential: aws.Bool(false),
		Image:     aws.String(postmanECRImage),
	})

	output, err := wf.ecsClient.RegisterTaskDefinition(wf.ctx, input)
	if err != nil {
		if uoe, unauth := isUnauthorized(err); unauth {
			printer.Errorf("The provided credentials do not have permission to register an ECS task definition (operation %s).\n",
				uoe.OperationName)
			printer.Infof("Please start over with a different profile, or add this permission in IAM.\n")
			return awf_error(errors.New("Failed to update the ECS task definition due to insufficient permissions."))
		}
		printer.Errorf("Could not register an ECS task definition. The error from the AWS library is shown below. Please send this log message to observability-support@postman.com for assistance.\n", err)
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
		printer.Errorf("Could not update the ECS service %q. The error from the AWS library is shown below. Please send this log message to observability-support@postman.com for assistance.\n", wf.ecsServiceARN, err)
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
		return awf_error(errors.New("The modified task failed to deploy. Please contact observability-support@postman.com for assistance."))
	}

	reportStep("ECS Service Updated")
	printer.Infof("Deployment successful! Please return to the Postman Live collection you created.\n")
	return awf_done()
}
