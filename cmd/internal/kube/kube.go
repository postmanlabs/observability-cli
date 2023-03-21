package kube

import (
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "kube",
	Short: "Install Akita in your Kubernetes cluster",
	Aliases: []string{
		"k8s",
		"kubernetes",
	},
	RunE: func(_ *cobra.Command, _ []string) error {
		return cmderr.AkitaErr{Err: errors.New("no subcommand specified")}
	},
}
