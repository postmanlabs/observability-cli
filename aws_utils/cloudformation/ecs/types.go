package ecs_cloudformation_utils

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/akitasoftware/go-utils/slices"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"gopkg.in/yaml.v2"
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
	Name        *string        `json:"Name,omitempty" yaml:"Name,omitempty"`
	Image       *string        `json:"Image,omitempty" yaml:"Image,omitempty"`
	Environment []keyValuePair `json:"Environment,omitempty" yaml:"Environment,omitempty"`
	EntryPoint  []string       `json:"EntryPoint,omitempty" yaml:"EntryPoint,omitempty"`
	Essential   *bool          `json:"Essential,omitempty" yaml:"Essential,omitempty"`
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
	Name  *string `json:"Name,omitempty" yaml:"Name,omitempty"`
	Value *string `json:"Value,omitempty" yaml:"Value,omitempty"`
}

func convertKeyValuePair(kv types.KeyValuePair) keyValuePair {
	return keyValuePair{
		Name:  kv.Name,
		Value: kv.Value,
	}
}

func ContainerDefinitionToJSONForCloudFormation(
	cd types.ContainerDefinition,
) (string, error) {
	// Indent five levels to line up with expected indent level of other container
	// definitions in a task definition.
	prefix := "                    "
	result, err := json.MarshalIndent(convertContainerDefinition(cd), prefix, "    ")
	result = append([]byte(prefix), result...)
	return string(result), err
}

func ContainerDefinitionToYAMLForCloudFormation(
	cd types.ContainerDefinition,
) (string, error) {
	// Put the container definition in a list, so it can be easily appended to an
	// existing list of container definitions.
	containerDefs := []containerDefinition{
		convertContainerDefinition(cd),
	}

	yamlBytes, err := yaml.Marshal(
		containerDefs,
	)
	if err != nil {
		return "", err
	}

	// Trim off any extraneous newlines.
	yamlBytes = bytes.Trim(yamlBytes, "\n")

	// Indent four levels to line up with the expected indent level of the other
	// container definitions in a task definition.
	prefix := "        "
	result := prefix + strings.ReplaceAll(string(yamlBytes), "\n", "\n"+prefix)
	return result, nil
}
