package setversion

import (
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/setversion"
	"github.com/akitasoftware/akita-libs/akiuri"
	"github.com/akitasoftware/akita-libs/version_names"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Deprecated:   "API models are now created automatically by time range.",
	Use:          "setversion NAME SPEC_AKITA_URI",
	Short:        "Sets the version name for an API model.",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// First argument is the version.
		version := args[0]

		// Second argument must be a model URI.
		modelURI, err := akiuri.Parse(args[1])

		if err != nil {
			return errors.Wrapf(err, "%q is not a well-formed AkitaURI", args[1])
		}
		if !modelURI.ObjectType.IsSpec() {
			return errors.New("Must specify an API model. For example, \"akita://projectName:spec:specName\"")
		}

		// Check for reserved versions.
		if version_names.IsReservedVersionName(version) {
			return errors.Errorf("'%s' is an Akita-reserved version", version)
		}

		setversionArgs := setversion.Args{
			ClientID:    akiflag.GetClientID(),
			Domain:      akiflag.Domain,
			ModelURI:    modelURI,
			VersionName: version,
		}

		if err := setversion.Run(setversionArgs); err != nil {
			return cmderr.AkitaErr{Err: err}
		}

		return nil
	},
}
