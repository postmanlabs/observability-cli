package ecs

import (
	"context"

	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

// Utility method for: listing a bunch of ARNs, and then looking up their names.
//
// Example: in a region, list all clusters.
// Example: in a cluster, list all services.
//

// Interface of the Paginator returned by the ECS package.  TODO: should we make
// this take the Options type as an extra parameter, so it can be used with other packages?
type AWSPaginator[ListOutput any] interface {
	HasMorePages() bool
	NextPage(context.Context, ...func(*ecs.Options)) (ListOutput, error)
}

func ListAWSObjectsByName[
	ListOutput any, // an SDK output type for the List operation
	DescribeInput any, // an SDK input type for the Describe operation
	DescribeOutput any, // an SDK output type for the Describe operation
	NamedItem any, // The type of the field within the Describe output
](
	ctx context.Context,
	objectName string, // "Task" or "Cluster" etc. for debugging
	paginator AWSPaginator[ListOutput], // Paginator for the List<Object> call
	getArns func(ListOutput) []arn, // Convert list output to list of ARNs
	inputFactory func([]arn) DescribeInput, // Convert list of ARNs to an input to the describe function
	describe func(context.Context, DescribeInput) (DescribeOutput, error), // SDK function to call Describe<Object>
	extract func(DescribeOutput) []NamedItem, // Extract a list of ARN/name pairs from the Describe output
	convert func(NamedItem) (arn, string), // Convert the ARN/name pair to a usable form, return "" if not present
) (map[arn]string, error) {

	arns := make([]arn, 0)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			telemetry.Error("AWS ECS List"+objectName, err)
			return nil, wrapUnauthorized(err)
		}
		arns = append(arns, getArns(output)...)
	}

	if len(arns) == 0 {
		return nil, nil
	}

	numARNs := len(arns)
	ret := make(map[arn]string, numARNs)

	// Handle in batches of 100; all existing use cases support this as a maximum.
	for start := 0; start < numARNs; start += 100 {
		end := start + 100
		if end > numARNs {
			end = numARNs
		}
		describeInput := inputFactory(arns[start:end])
		describeResult, err := describe(ctx, describeInput)
		if err != nil {
			telemetry.Error("AWS ECS Describe"+objectName, err)
			return nil, wrapUnauthorized(err)
		}

		for _, c := range extract(describeResult) {
			arn, name := convert(c)
			if arn != "" {
				ret[arn] = name
			}
		}
	}

	return ret, nil
}
