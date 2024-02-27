package kube

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Deprecated: "This is no longer supported and might be removed in a future release.",
	Use:        "kube",
	Short:      "Install the Postman Live Collections Agent in your Kubernetes cluster",
	Aliases: []string{
		"k8s",
		"kubernetes",
	},
}
