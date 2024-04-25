package ecs_console_utils

import (
	"encoding/json"

	"github.com/akitasoftware/go-utils/slices"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// A type whose JSON representation is suitable for use with the AWS console for
// creating a ECS task definition.
//
// XXX This is incomplete. Currently, only those fields we use are listed here.
//
// The fields here are taken from
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-definition-template.html.
// Their types correspond to those defined in the AWS SDK v2.
type taskDefinition struct {
	Family                  *string               `json:"family,omitempty"`
	NetworkMode             types.NetworkMode     `json:"networkMode,omitempty"`
	ContainerDefinitions    []containerDefinition `json:"containerDefinitions,omitempty"`
	RequiresCompatibilities []types.Compatibility `json:"requiresCompatibilities,omitempty"`
	CPU                     *string               `json:"cpu,omitempty"`
	Memory                  *string               `json:"memory,omitempty"`
	RuntimePlatform         *runtimePlatform      `json:"runtimePlatform,omitempty"`
}

func convertTaskDefinition(td types.TaskDefinition) taskDefinition {
	return taskDefinition{
		Family:                  td.Family,
		NetworkMode:             td.NetworkMode,
		ContainerDefinitions:    slices.Map(td.ContainerDefinitions, convertContainerDefinition),
		RequiresCompatibilities: td.RequiresCompatibilities,
		CPU:                     td.Cpu,
		Memory:                  td.Memory,
		RuntimePlatform:         convertRuntimePlatform(td.RuntimePlatform),
	}
}

// A container definition within a task definition. The JSON representation of
// this type is suitable for use with the AWS console.
//
// XXX This is incomplete. Currently, only those fields we use are listed here.
//
// The fields here are taken from
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-definition-template.html.
// Their types correspond to those defined in the AWS SDK v2.
type containerDefinition struct {
	Name        *string        `json:"name,omitempty"`
	Image       *string        `json:"image,omitempty"`
	Essential   *bool          `json:"essential,omitempty"`
	EntryPoint  []string       `json:"entryPoint,omitempty"`
	Environment []keyValuePair `json:"environment,omitempty"`
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

// The JSON representation of this type is suitable for use with the AWS
// console.
//
// The fields here are taken from
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-definition-template.html.
// Their types correspond to those defined in the AWS SDK v2.
type keyValuePair struct {
	Name  *string `json:"name,omitempty"`
	Value *string `json:"value,omitempty"`
}

func convertKeyValuePair(kv types.KeyValuePair) keyValuePair {
	return keyValuePair{
		Name:  kv.Name,
		Value: kv.Value,
	}
}

// The JSON representation of this type is suitable for use with the AWS
// console.
//
// The fields here are taken from
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-definition-template.html.
// Their types correspond to those defined in the AWS SDK v2.
type runtimePlatform struct {
	CpuArchitecture       types.CPUArchitecture `json:"cpuArchitecture,omitempty"`
	OperatingSystemFamily types.OSFamily        `json:"operatingSystemFamily,omitempty"`
}

func convertRuntimePlatform(rp *types.RuntimePlatform) *runtimePlatform {
	return &runtimePlatform{
		CpuArchitecture:       rp.CpuArchitecture,
		OperatingSystemFamily: rp.OperatingSystemFamily,
	}
}

func TaskDefinitionToJSONForConsole(td types.TaskDefinition) ([]byte, error) {
	return json.MarshalIndent(convertTaskDefinition(td), "", "  ")
}
