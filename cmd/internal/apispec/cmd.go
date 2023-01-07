package apispec

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
)

var Cmd = &cobra.Command{
	Use:                "apispec",
	SilenceUsage:       true,
	DisableFlagParsing: true,
	Hidden:             true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmderr.AkitaErr{Err: errors.New("the apispec command has been removed. Please use apidump instead")}
	},
}
