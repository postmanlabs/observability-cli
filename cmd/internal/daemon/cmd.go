package daemon

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-cli/daemon"
	"github.com/akitasoftware/akita-libs/akid"
)

var (
	// Optional flags
	portNumberFlag uint16

	pluginsFlag []string
)

var Cmd = &cobra.Command{
	Use:          "daemon",
	Short:        "Run the Akita client daemon.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		plugins, err := pluginloader.Load(pluginsFlag)
		if err != nil {
			return errors.Wrap(err, "Failed to load plugins")
		}

		args := daemon.Args{
			ClientID:   akid.GenerateClientID(),
			Domain:     akiflag.Domain,
			PortNumber: portNumberFlag,

			Plugins: plugins,
		}

		if err := daemon.Run(args); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}

func init() {
	Cmd.Flags().Uint16Var(
		&portNumberFlag,
		"port",
		50_080,
		"The port number on which to listen for connections.",
	)
}
