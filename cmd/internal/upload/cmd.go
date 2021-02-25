package upload

import (
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-libs/akid"

	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/upload"
)

var Cmd = &cobra.Command{
	Use:          "upload [PATH_TO_SPEC]",
	Short:        "Upload API spec to Akita.",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		uploadArgs := upload.Args{
			ClientID:      akid.GenerateClientID(),
			Domain:        akiflag.Domain,
			Service:       serviceFlag,
			SpecPath:      args[0],
			SpecName:      specNameFlag,
			UploadTimeout: uploadTimeoutFlag,
		}

		if err := upload.Run(uploadArgs); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}
