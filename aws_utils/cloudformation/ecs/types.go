package ecs_cloudformation_utils

import (
	"encoding/json"

	"github.com/akitasoftware/go-utils/slices"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// The JSON and YAML representations of this type are suitable for use in AWS
// CloudFormation templates.
//
// XXX This is incomplete. Currently, only those fields we use are listed here.
//
// The fields here are taken from
// https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-ecs-taskdefinition-containerdefinition.html.
// Their types correspond to those defined in the AWS SDK v2.
type containerDefinition struct {
	Environment []keyValuePair `json:"Environment,omitempty"`
	EntryPoint  []string       `json:"EntryPoint,omitempty"`
	Essential   *bool          `json:"Essential,omitempty"`
	Image       *string        `json:"Image,omitempty"`
	Name        *string        `json:"Name,omitempty"`
}

func convertContainerDefinition(cd types.ContainerDefinition) containerDefinition {
	return containerDefinition{
		Name:        cd.Name,
		Image:       cd.Image,
		Essential:   cd.Essential,
		EntryPoint:  cd.EntryPoint,
		Environment: slices.Map(cd.Environment, convertKeyValuePair),
	}
}

// The JSON and YAML representations of this type are suitable for use in AWS
// CloudFormation templates.
//
// The fields here are taken from
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-definition-template.html.
// Their types correspond to those defined in the AWS SDK v2.
type keyValuePair struct {
	Name  *string `json:"Name,omitempty"`
	Value *string `json:"Value,omitempty"`
}

func convertKeyValuePair(kv types.KeyValuePair) keyValuePair {
	return keyValuePair{
		Name:  kv.Name,
		Value: kv.Value,
	}
}

func ContainerDefinitionToJSONForCloudformation(
	cd types.ContainerDefinition,
) ([]byte, error) {
	// Indent five levels to line up with expected indent level of other container
	// definitions in a task definition.
	prefix := "                    "
	result, err := json.MarshalIndent(convertContainerDefinition(cd), prefix, "    ")
	result = append([]byte(prefix), result...)
	return result, err
}
