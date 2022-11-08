package ecs

import (
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
)

// Workflow methods that use the AWS API.
// Some of these may be refactored so they can be shared with the "remove" workflow.

// Check whether we can find crentials for the specified profile
func (wf *AddWorkflow) checkCredentials() error {
	_, err := wf.awsCred.Get()
	return err
}

// Create a session from the credentials and region in the workflow.
func (wf *AddWorkflow) createSession() error {
	var sessionError error
	wf.session, sessionError = session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Credentials: wf.awsCred,
			Region:      aws.String(wf.awsRegion),
		},
	})
	if sessionError != nil {
		telemetry.Error("AWS CreateSession", sessionError)
	}
	return sessionError
}

// Create a session from the credentials in the workflow and the region in the user's config file.
func (wf *AddWorkflow) createSessionFromConfig() error {
	var sessionError error
	wf.session, sessionError = session.NewSessionWithOptions(session.Options{
		Profile: awsProfileFlag,
		Config: aws.Config{
			Credentials: wf.awsCred,
		},
		SharedConfigState: session.SharedConfigEnable,
	})
	if sessionError != nil {
		telemetry.Error("AWS CreateSession", sessionError)
	}
	return sessionError
}

// List all clusters for the current region, by arn and user-assigned name
func (wf *AddWorkflow) listECSClusters() (map[arn]string, error) {
	svc := ecs.New(wf.session)
	input := &ecs.ListClustersInput{}

	result, err := svc.ListClusters(input)
	if err != nil {
		telemetry.Error("AWS EC2 ListClusters", err)
		// do not wrap, so that caller can access the awserr.Error.Code()
		return nil, err
	}

	if len(result.ClusterArns) == 0 {
		return nil, nil
	}

	numArns := len(result.ClusterArns)
	printer.Infof("Found %d clusters in region %q.\n", numArns, wf.awsRegion)

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
		describeResult, err := svc.DescribeClusters(describeInput)
		if err != nil {
			telemetry.Error("AWS EC2 DescribeClusters", err)
			return nil, err
		}

		// Ignore any failures in the result?
		// TODO: check whether they are actually permission errors or something else?
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
