package deployment

import (
	"os"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/tags"
)

// Internal type of the deployment, automatically discovered.
// The most-specific one is returned.  (Or do we need to
// support categorizing the deployment in more than one way?)
// This does not correspond to a tag.
type Deployment string

const (
	None       Deployment = ""
	Any        Deployment = "any"
	Unknown    Deployment = "unknown"
	AWS        Deployment = "aws"
	AWS_ECS    Deployment = "aws-ecs"
	Kubernetes Deployment = "kubernetes"
)

// Map of environment variables to tags, grouped by deployment type.
var environmentToTag map[Deployment]map[string]tags.Key = map[Deployment]map[string]tags.Key{
	Any: {
		"AKITA_DEPLOYMENT_COMMIT": tags.XAkitaGitCommit,
	},
	AWS: {
		"AKITA_AWS_REGION": tags.XAkitaAWSRegion,
	},
	AWS_ECS: {
		"AKITA_AWS_REGION":  tags.XAkitaAWSRegion,
		"AKITA_ECS_TASK":    tags.XAkitaECSTask,
		"AKITA_ECS_SERVICE": tags.XAkitaECSService,
	},
	Kubernetes: {
		"AKITA_K8S_NAMESPACE": tags.XAkitaKubernetesNamespace,
		"AKITA_K8S_NODE":      tags.XAkitaKubernetesNode,
		"AKITA_K8S_HOST_IP":   tags.XAkitaKubernetesHostIP,
		"AKITA_K8S_POD":       tags.XAkitaKubernetesPod,
		"AKITA_K8S_POD_IP":    tags.XAkitaKubernetesPodIP,
		"AKITA_K8S_DAEMONSET": tags.XAkitaKubernetesDaemonset,
	},
}

func (d Deployment) String() string {
	return string(d)
}

// Use envToTag map to see if any of the environment variables are present.
// Return true if so, and update the tagset.
func (d Deployment) getTagsFromEnvironment(tagset map[tags.Key]string) bool {
	found := false
	for envVar, tag := range environmentToTag[d] {
		if v := os.Getenv(envVar); v != "" {
			tagset[tag] = v
			found = true
		}
	}
	return found
}

func GetDeploymentInfo() (Deployment, map[tags.Key]string) {
	deploymentType := None
	tagset := make(map[tags.Key]string)

	// Allow the user to specify the name (not type) of deployment environment,
	// even if it's of an unknown type.
	// If there is a git commit associated with this deployment, then record it.
	if Any.getTagsFromEnvironment(tagset) {
		deploymentType = Unknown
	}

	if AWS_ECS.getTagsFromEnvironment(tagset) {
		printer.Infof("Found AWS ECS environment variables.\n")
		deploymentType = AWS_ECS
	} else if AWS.getTagsFromEnvironment(tagset) {
		printer.Infof("Found AWS environment variables.\n")
		deploymentType = AWS
	}

	if Kubernetes.getTagsFromEnvironment(tagset) {
		printer.Infof("Found Kubernetes environment variables.\n")
		deploymentType = Kubernetes
	}

	return deploymentType, tagset
}

// Import information about production or staging environment
// if it is available in environment variables.
func UpdateTags(argsTags map[tags.Key]string) {
	deploymentType, deploymentTags := GetDeploymentInfo()

	// Only specify source if no source is already set.
	if deploymentType != None {
		if _, present := argsTags[tags.XAkitaSource]; !present {
			argsTags[tags.XAkitaSource] = tags.DeploymentSource
		}
	}

	// Copy into existing map
	for k, v := range deploymentTags {
		argsTags[k] = v
	}
}
