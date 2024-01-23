package ecs

import (
	"errors"
	"fmt"

	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	smithy "github.com/aws/smithy-go"
)

// Workflow methods that use the AWS API.
// Some of these may be refactored so they can be shared with the "remove" workflow.

// Error indicating profile is absent.
// TODO: stop mixing error.Is and error.As usage?
var NoSuchProfileError = errors.New("No such profile")
var NoSuchClusterError = errors.New("No such cluster")

// Error indicating the user was unauthorized.
type UnauthorizedOperationError struct {
	ServiceID     string
	OperationName string
	ARN           arn
}

func (e UnauthorizedOperationError) Error() string {
	if e.ARN == "" {
		return fmt.Sprintf("The selected profile is unauthorized to perform %v on %v", e.OperationName, e.ServiceID)
	} else {
		return fmt.Sprintf("The selected profile is unauthorized to perform %v on %v", e.OperationName, e.ARN)
	}
}

// Check whether the API error code was "UnauthorizedOperation" and convert to
// an UnauthorizedOperationError if so.
func isUnauthorized(err error) (UnauthorizedOperationError, bool) {
	var apiError smithy.APIError
	var opError *smithy.OperationError
	if errors.As(err, &apiError) {
		if errors.As(err, &opError) {
			if apiError.ErrorCode() == "UnauthorizedOperation" ||
				apiError.ErrorCode() == "AccessDeniedException" ||
				// UnrecognizedClientException happens if a token is not recognized in that region
				apiError.ErrorCode() == "UnrecognizedClientException" {
				return UnauthorizedOperationError{
					ServiceID:     opError.ServiceID,
					OperationName: opError.OperationName,
				}, true
			}
		}
	}
	return UnauthorizedOperationError{}, false
}

func isUnauthorizedFor(err error, arn arn) (UnauthorizedOperationError, bool) {
	e, ok := isUnauthorized(err)
	if ok {
		e.ARN = arn
	}
	return e, ok
}

func wrapUnauthorized(err error) error {
	uoe, ok := isUnauthorized(err)
	if ok {
		return uoe
	}
	return err
}

func wrapUnauthorizedFor(err error, arn arn) error {
	uoe, ok := isUnauthorizedFor(err, arn)
	if ok {
		return uoe
	}
	return err
}

// Check whether we can load the config object
func (wf *AddWorkflow) createConfig() error {
	// Try loading credentials for the profile.
	// Use the default if running standalone.  Add /aws for use inside a container.
	configFiles := []string{config.DefaultSharedConfigFilename(), "/aws/config"}
	credentialsFiles := []string{config.DefaultSharedCredentialsFilename(), "/aws/credentials"}
	if awsCredentialsFlag != "" {
		credentialsFiles = append(credentialsFiles, awsCredentialsFlag)
	}

	sharedConfig, err := config.LoadSharedConfigProfile(wf.ctx, wf.awsProfile,
		func(options *config.LoadSharedConfigOptions) {
			options.CredentialsFiles = credentialsFiles
			options.ConfigFiles = configFiles
		},
	)
	if err != nil {
		telemetry.Error("Load Shared Config Profile", err)
		if _, ok := err.(config.SharedConfigProfileNotExistError); ok {
			return NoSuchProfileError
		}
		return err
	}
	wf.awsRegion = sharedConfig.Region

	// TODO: is there some way we can make this *not* include IMDS as a credentials provider?
	cfg, err := config.LoadDefaultConfig(wf.ctx,
		config.WithSharedConfigProfile(wf.awsProfile),
		config.WithSharedConfigFiles(configFiles),
		config.WithSharedCredentialsFiles(credentialsFiles),
	)
	if err != nil {
		return err
	}
	wf.awsConfig = cfg

	return nil
}

// Create an ECS client with the specified region
func (wf *AddWorkflow) createClient(region string) {
	wf.awsConfig.Region = region
	wf.ecsClient = ecs.NewFromConfig(wf.awsConfig)
}

// Create an ECS client assuming that the config has a default region.
func (wf *AddWorkflow) createClientWithDefaultRegion() {
	wf.ecsClient = ecs.NewFromConfig(wf.awsConfig)
}

var publicAWSRegions = []string{
	"af-south-1",
	"ap-east-1",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-northeast-3",
	"ap-south-1",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-southeast-3",
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


// Verify that a cluster exists with the given ARN.
// Returns its name, or else NoSuchClusterError if search is empty.
func (wf *AddWorkflow) getClusterName(cluster arn) (string, error) {
	describeInput := &ecs.DescribeClustersInput{
		Clusters: []string{string(cluster)},
	}
	describeResult, err := wf.ecsClient.DescribeClusters(wf.ctx, describeInput)
	if err != nil {
		telemetry.Error("AWS ECS DescribeClusters", err)
		if uoe, ok := isUnauthorizedFor(err, cluster); ok {
			return "", uoe
		}
		return "", err
	}

	if len(describeResult.Clusters) == 0 {
		return "", NoSuchClusterError
	}

	for _, c := range describeResult.Clusters {
		if c.ClusterName != nil {
			return *c.ClusterName, nil
		}
	}
	return "", nil
}

func arnsToStrings(arns []arn) []string {
	ret := make([]string, len(arns))
	for i, a := range arns {
		ret[i] = string(a)
	}
	return ret
}

func stringsToArns(arns []string) []arn {
	ret := make([]arn, len(arns))
	for i, a := range arns {
		ret[i] = arn(a)
	}
	return ret
}

// Look up ECS task definition using ARN
func (wf *AddWorkflow) getECSTaskDefinition(ecsTaskDefinitionARN arn) (*types.TaskDefinition, []types.Tag, error) {
	input := &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: ecsTaskDefinitionARN.Use(),
	}

	output, err := wf.ecsClient.DescribeTaskDefinition(wf.ctx, input)
	if err != nil {
		telemetry.Error("AWS ECS DescribeTaskDefinition", err)
		return nil, nil, wrapUnauthorized(err)
	}
	return output.TaskDefinition, output.Tags, nil
}

// Look up a ECS service using ARN
func (wf *AddWorkflow) getService(serviceARN arn) (*types.Service, error) {
	input := &ecs.DescribeServicesInput{
		Services: []string{string(serviceARN)},
		Cluster:  wf.ecsClusterARN.Use(),
	}

	output, err := wf.ecsClient.DescribeServices(wf.ctx, input)
	if err != nil {
		telemetry.Error("AWS ECS DescribeServices", err)
		return nil, wrapUnauthorizedFor(err, serviceARN)
	}
	if len(output.Services) == 0 {
		return nil, fmt.Errorf("no service with ARN %q", serviceARN)
	}
	return &output.Services[0], nil
}

var noDeploymentFound = errors.New("No deployment found")

// Returns the deployment of the given service that matches the ECS task
// definition configured in the workflow. For convenience, the deployment ID
// is also returned as a string.
func (wf *AddWorkflow) GetDeploymentMatchingTask(serviceARN arn) (string, types.Deployment, error) {
	service, err := wf.getService(serviceARN)
	if err != nil {
		return "", types.Deployment{}, err
	}

	for _, d := range service.Deployments {
		if aws.ToString(d.TaskDefinition) == string(wf.ecsTaskDefinitionARN) {
			return aws.ToString(d.Id), d, nil

		}
	}

	return "", types.Deployment{}, errors.New("no deployment found")
}

func (wf *AddWorkflow) GetDeploymentByID(serviceARN arn, deploymentID string) (types.Deployment, error) {
	service, err := wf.getService(serviceARN)
	if err != nil {
		return types.Deployment{}, err
	}

	for _, d := range service.Deployments {
		if aws.ToString(d.Id) == deploymentID {
			return d, nil
		}
	}

	return types.Deployment{}, errors.New("No deployment found")
}
