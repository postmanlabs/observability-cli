package deployment

import (
	"os"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/tags"
)

type Deployment string

const (
	None       Deployment = ""
	Unknown               = "unknown"
	Kubernetes            = "kubernetes"
)

func (d Deployment) String() string {
	return string(d)
}

func GetDeploymentInfo() (Deployment, map[tags.Key]string) {
	// Allow the user to specify a deployment environment, even
	// if it's of an unknown type.

	deploymentType := None
	tagset := make(map[tags.Key]string)

	if d := os.Getenv("AKITA_DEPLOYMENT"); d != "" {
		deploymentType = Unknown
		tagset[tags.XAkitaDeployment] = d
	}

	// If there is a git commit associated with this deployment,
	// then record it.
	if c := os.Getenv("AKITA_DEPLOYMENT_COMMIT"); c != "" {
		tagset[tags.XAkitaGitCommit] = c
	}

	if GetAWSTags(tagset) {
		printer.Infof("Found AWS environment variables.\n")
	}

	if GetKubernetesTags(tagset) {
		printer.Infof("Found Kubernetes environment variables.\n")
		deploymentType = Kubernetes
	}

	return deploymentType, tagset
}

func GetKubernetesTags(tagset map[tags.Key]string) bool {
	// Return true if any of these are present.
	envMapping := []struct {
		EnvVar string
		Key    tags.Key
	}{
		{"AKITA_K8S_NAMESPACE", tags.XAkitaKubernetesNamespace},
		{"AKITA_K8S_NODE", tags.XAkitaKubernetesNode},
		{"AKITA_K8S_HOST_IP", tags.XAkitaKubernetesHostIP},
		{"AKITA_K8S_POD", tags.XAkitaKubernetesPod},
		{"AKITA_K8S_POD_IP", tags.XAkitaKubernetesPodIP},
		{"AKITA_K8S_DAEMONSET", tags.XAkitaKubernetesDaemonset},
	}

	found := false
	for _, e := range envMapping {
		if v := os.Getenv(e.EnvVar); v != "" {
			tagset[e.Key] = v
			found = true
		}
	}

	return found
}

func GetAWSTags(tagset map[tags.Key]string) bool {
	// Only the region for now

	if awsRegion := os.Getenv("AKITA_AWS_REGION"); awsRegion != "" {
		tagset[tags.XAkitaAWSRegion] = awsRegion
		return true
	}
	return false
}
