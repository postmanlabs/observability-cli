package apidiff

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/apidiff"
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-libs/akiuri"
)

// TODO(kku): support local specs by uploading them first.
var Cmd = &cobra.Command{
	Use:          "apidiff [BASE_SPEC_AKITA_URI] [NEW_SPEC_AKITA_URI]",
	Short:        "Compare 2 API specs.",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		spec1, err := akiuri.Parse(args[0])
		if err != nil {
			return errors.Wrapf(err, "%q is not a well-formed AkitaURI", args[0])
		}

		spec2, err := akiuri.Parse(args[1])
		if err != nil {
			return errors.Wrapf(err, "%q is not a well-formed AkitaURI", args[1])
		}

		diffArgs := apidiff.Args{
			ClientID:    akiflag.ClientID,
			Domain:      akiflag.Domain,
			BaseSpecURI: spec1,
			NewSpecURI:  spec2,
			Out:         outFlag,
		}
		if err := apidiff.Run(diffArgs); err != nil {
			return cmderr.AkitaErr{Err: err}
		}

		return nil
	},
}
