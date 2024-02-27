package daemon

import (
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-cli/daemon"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
)

var (
	// Required flags
	nameFlag string

	// Optional flags
	portNumberFlag uint16

	pluginsFlag []string
)

var Cmd = &cobra.Command{
	Deprecated:   "This is no longer supported and will be removed in a future release.",
	Use:          "daemon",
	Short:        "Run the Akita client daemon.",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		plugins, err := pluginloader.Load(pluginsFlag)
		if err != nil {
			return errors.Wrap(err, "Failed to load plugins")
		}

		args := daemon.Args{
			ClientID:   telemetry.GetClientID(),
			Domain:     rest.Domain,
			DaemonName: nameFlag,
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
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}

	Cmd.Flags().StringVar(
		&nameFlag,
		"name",
		hostname,
		"The name of the daemon. Used to identify this daemon in Akita Cloud. Only required if the CLI is unable to determine the hostname.",
	)
	if err != nil {
		cobra.MarkFlagRequired(Cmd.Flags(), "name")
	}

	Cmd.Flags().Uint16Var(
		&portNumberFlag,
		"port",
		50_080,
		"The port number on which to listen for connections.",
	)
}
