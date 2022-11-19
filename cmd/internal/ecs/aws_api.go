package ecs

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
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

// List all regions in alphabetical order. On error fall back to the precanned list.
func (wf *AddWorkflow) listAWSRegions() (result []string) {
	defer func() { sort.Strings(result) }()

	// Need a region to list the regions, unfortunately.
	if wf.awsConfig.Region == "" {
		wf.awsConfig.Region = "us-east-1"
	}

	ec2Client := ec2.NewFromConfig(wf.awsConfig)

	out, err := ec2Client.DescribeRegions(wf.ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false),
	})
	if err != nil {
		telemetry.Error("AWS EC2 DescribeRegions", err)

		if _, ok := isUnauthorized(err); ok {
			printer.Warningf("Failed to list available regions, because you are not authorized to make the DescribeRegions call in region %v. Falling back to a precompiled list.\n", wf.awsConfig.Region)
			return publicAWSRegions
		}

		printer.Warningf("Failed to list available regions from AWS; falling back to a precompiled list. Error was: %v\n", err)
		return publicAWSRegions
	}

	if len(out.Regions) == 0 {
		// Could a user have all regions disabled?
		printer.Warningf("List of available regions from AWS was empty. Falling back to a precompiled list.\n", err)
		return publicAWSRegions
	}

	ret := make([]string, 0, len(out.Regions))
	for _, r := range out.Regions {
		if r.RegionName != nil {
			ret = append(ret, *r.RegionName)
		}
	}
	return ret
}

// List all clusters for the current region, by arn and user-assigned name
func (wf *AddWorkflow) listECSClusters() (map[arn]string, error) {
	input := &ecs.ListClustersInput{}
	return ListAWSObjectsByName[
		*ecs.ListClustersOutput,
		*ecs.DescribeClustersInput,
		*ecs.DescribeClustersOutput,
		types.Cluster](
		wf.ctx,
		"Clusters",
		ecs.NewListClustersPaginator(wf.ecsClient, input),
		func(output *ecs.ListClustersOutput) []arn {
			return stringsToArns(output.ClusterArns)
		},
		func(arns []arn) *ecs.DescribeClustersInput {
			return &ecs.DescribeClustersInput{
				Clusters: arnsToStrings(arns),
			}
		},
		func(ctx context.Context, input *ecs.DescribeClustersInput) (*ecs.DescribeClustersOutput, error) {
			return wf.ecsClient.DescribeClusters(ctx, input)
		},
		func(output *ecs.DescribeClustersOutput) []types.Cluster {
			return output.Clusters
		},
		func(t types.Cluster) (arn, string) {
			return arn(aws.ToString(t.ClusterArn)), aws.ToString(t.ClusterName)
		},
	)
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

// List all tasks definition families, by name
func (wf *AddWorkflow) listECSTaskDefinitionFamilies() ([]string, error) {
	// TODO: Lists only active tasks, should we permit inactive ones too?
	input := &ecs.ListTaskDefinitionFamiliesInput{
		Status: types.TaskDefinitionFamilyStatusActive,
	}

	families := make([]string, 0)
	paginator := ecs.NewListTaskDefinitionFamiliesPaginator(wf.ecsClient, input)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(wf.ctx)
		if err != nil {
			telemetry.Error("AWS ECS ListTaskDefinitionFamilies", err)
			return nil, wrapUnauthorized(err)
		}
		families = append(families, output.Families...)
	}
	return families, nil
}

// Look up the most recent version of a task definition
func (wf *AddWorkflow) getLatestECSTaskDefinition(family string) (*types.TaskDefinition, []types.Tag, error) {
	input := &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(family),
	}

	output, err := wf.ecsClient.DescribeTaskDefinition(wf.ctx, input)
	if err != nil {
		telemetry.Error("AWS ECS DescribeTaskDefinition", err)
		return nil, nil, wrapUnauthorized(err)
	}
	return output.TaskDefinition, output.Tags, nil
}

// List all services for the current cluster, by arn and user-assigned name
// Filter to only those using the task family we identified!
func (wf *AddWorkflow) listECSServices() (map[arn]string, error) {
	// Lists both Fargate and ECS services
	input := &ecs.ListServicesInput{
		Cluster: wf.ecsClusterARN.Use(),
	}

	// Cache of ARN to family
	arnToFamily := map[arn]string{
		wf.ecsTaskDefinitionARN: wf.ecsTaskDefinitionFamily,
	}

	// Check whether the given Task ARN has Family equal to that of the chosen task definition.
	taskInFamily := func(serviceARN arn, taskARN arn) bool {
		if family, cached := arnToFamily[taskARN]; cached {
			return family == wf.ecsTaskDefinitionFamily
		}
		input := &ecs.DescribeTaskDefinitionInput{
			TaskDefinition: taskARN.Use(),
		}
		output, err := wf.ecsClient.DescribeTaskDefinition(wf.ctx, input)
		if err != nil {
			telemetry.Error("AWS ECS DescribeTaskDefinition", err)
			if uoe, unauth := isUnauthorized(err); unauth {
				printer.Warningf("Skipping service %q because the provided credentials are unauthorized for %s on %q.\n",
					serviceARN, uoe.OperationName, taskARN)
			} else {
				printer.Warningf("Skipping service %q because of an error checking its task definition: %v\n", serviceARN, err)
			}
			return false
		}
		family := aws.ToString(output.TaskDefinition.Family)
		arnToFamily[taskARN] = family
		return family == wf.ecsTaskDefinitionFamily
	}

	// Include only those services sharing the correct family.
	filterFunc := func(output *ecs.DescribeServicesOutput) []types.Service {
		filtered := make([]types.Service, 0)
		for _, s := range output.Services {
			// s.TaskDefinition is an ARN, but we want to match by family.
			// We could try parsing the ARN? But I think the correct route is
			// to look up the task definition, if unknown.
			if taskInFamily(arn(aws.ToString(s.ServiceArn)), arn(aws.ToString(s.TaskDefinition))) {
				filtered = append(filtered, s)
			}
		}
		return filtered
	}

	return ListAWSObjectsByName[
		*ecs.ListServicesOutput,
		*ecs.DescribeServicesInput,
		*ecs.DescribeServicesOutput,
		types.Service](
		wf.ctx,
		"Services",
		ecs.NewListServicesPaginator(wf.ecsClient, input),
		func(output *ecs.ListServicesOutput) []arn {
			return stringsToArns(output.ServiceArns)
		},
		func(arns []arn) *ecs.DescribeServicesInput {
			return &ecs.DescribeServicesInput{
				Cluster:  wf.ecsClusterARN.Use(),
				Services: arnsToStrings(arns),
			}
		},
		func(ctx context.Context, input *ecs.DescribeServicesInput) (*ecs.DescribeServicesOutput, error) {
			return wf.ecsClient.DescribeServices(ctx, input)
		},
		filterFunc,
		func(t types.Service) (arn, string) {
			return arn(aws.ToString(t.ServiceArn)), aws.ToString(t.ServiceName)
		},
	)
}

// Look up a service and check that its task definition matches.
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
		return nil, fmt.Errorf("No service with ARN %q", serviceARN)
	}
	return &output.Services[0], nil
}

func (wf *AddWorkflow) getServiceWithMatchingTask(serviceARN arn) (*types.Service, error) {
	svc, err := wf.getService(serviceARN)
	if err != nil {
		return nil, err
	}

	taskARN := arn(aws.ToString(svc.TaskDefinition))
	taskInput := &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: taskARN.Use(),
	}
	taskOutput, err := wf.ecsClient.DescribeTaskDefinition(wf.ctx, taskInput)
	if err != nil {
		telemetry.Error("AWS ECS DescribeTaskDefinition", err)
		return nil, wrapUnauthorizedFor(err, taskARN)
	}

	family := aws.ToString(taskOutput.TaskDefinition.Family)
	if family != wf.ecsTaskDefinitionFamily {
		printer.Warningf("Service %q has task definition %q, which does not match family %q.",
			serviceARN, taskARN, wf.ecsTaskDefinitionFamily)
		return nil, fmt.Errorf("Mismatch between service and task definition; please choose a different task or service.")
	}

	return svc, nil
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

	return "", types.Deployment{}, errors.New("No deployment found")
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
