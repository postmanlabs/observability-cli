package ecs

import (
	"errors"
	"fmt"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	smithy "github.com/aws/smithy-go"
)

// Workflow methods that use the AWS API.
// Some of these may be refactored so they can be shared with the "remove" workflow.

// Error indicating profile is absent.
// TODO: stop mixing error.Is and error.As usage?
var NoSuchProfileError = errors.New("No such profile")

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
				apiError.ErrorCode() == "AccessDeniedException" {
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

// Check whether we can load the config object
func (wf *AddWorkflow) createConfig() error {
	// Try loading credentials for the profile.
	// TODO: specify a location inside the Docker container, if running on Docker
	sharedConfig, err := config.LoadSharedConfigProfile(wf.ctx, wf.awsProfile)
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

// Create an ECS client asuming that the config has a default region.
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

// List all regions. On error fall back to the precanned list.
func (wf *AddWorkflow) listAWSRegions() []string {

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

	result, err := wf.ecsClient.ListClusters(wf.ctx, input)
	if err != nil {
		telemetry.Error("AWS ECS ListClusters", err)
		return nil, wrapUnauthorized(err)
	}

	if len(result.ClusterArns) == 0 {
		return nil, nil
	}

	numArns := len(result.ClusterArns)
	ret := make(map[arn]string, numArns)

	// Have to handle these in batches of 100.
	for start := 0; start < numArns; start += 100 {
		end := start + 100
		if end > numArns {
			end = numArns
		}
		describeInput := &ecs.DescribeClustersInput{
			Clusters: result.ClusterArns[start:end],
		}
		describeResult, err := wf.ecsClient.DescribeClusters(wf.ctx, describeInput)
		if err != nil {
			telemetry.Error("AWS ECS DescribeClusters", err)
			return nil, wrapUnauthorized(err)
		}

		// TODO: can we do anything about the Failures in the result?
		for _, c := range describeResult.Clusters {
			if c.ClusterArn != nil {
				if c.ClusterName != nil {
					ret[arn(*c.ClusterArn)] = *c.ClusterName
				} else {
					ret[arn(*c.ClusterArn)] = ""
				}
			}
		}
	}
	return ret, nil
}
