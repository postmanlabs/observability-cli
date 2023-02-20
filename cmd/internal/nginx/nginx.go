package nginx

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-cli/integrations/nginx"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
)

var (
	// Akita project name
	projectFlag string

	// Port number that the module will send traffic to
	listenPortFlag uint16

	// Dedvelopment mode -- dump out traffic locally
	developmentFlag bool
)

var Cmd = &cobra.Command{
	Use:          "nginx",
	Short:        "Install or use Akita's NGINX module to collect API traffic.",
	SilenceUsage: true,
	// TODO: un-hide when ready for use
	Hidden: true,
}

var CaptureCmd = &cobra.Command{
	Use:          "capture",
	Short:        "Capture traffic forwarded from Akita's NGINX module.",
	Long:         "Open a network port for communication with the Akita NGINX module. The agent will parse incoming traffic, obfuscate it, and forward it to the Akita Cloud.",
	SilenceUsage: true,
	RunE:         captureNginxTraffic,
}

var InstallCmd = &cobra.Command{
	// TODO: substitute in the real name
	Use:          "xinstall",
	Short:        "Download a precompiled NGINX module.",
	Long:         "Download a precompiled version of akita-nginx-module that matches the currently installed version of NGINX.",
	SilenceUsage: true,
	RunE:         installNginxModule,
}

func init() {
	Cmd.PersistentFlags().StringVar(&projectFlag, "project", "", "Your Akita project.")
	Cmd.PersistentFlags().Uint16Var(&listenPortFlag, "port", 50080, "The port number on which to listen for connections.")
	Cmd.PersistentFlags().BoolVar(&developmentFlag, "dev", false, "Enable development mode; only dumps traffic.")
	Cmd.PersistentFlags().MarkHidden("dev")

	Cmd.AddCommand(CaptureCmd)
	Cmd.AddCommand(InstallCmd)
}

func captureNginxTraffic(cmd *cobra.Command, args []string) error {
	if developmentFlag {
		return nginx.RunDevelopmentServer(listenPortFlag)
	}

	if projectFlag == "" {
		return errors.New("Must specify an Akita project name with the --project flag.")
	}

	// Get the default Akita plugins
	plugins, err := pluginloader.Load([]string{})
	if err != nil {
		return errors.Wrap(err, "failed to load plugins")
	}

	// TODO: filtering flags?
	// TODO: rate limit flags?

	nginxArgs := &nginx.Args{
		Domain:               rest.Domain,
		ClientID:             telemetry.GetClientID(),
		ServiceName:          projectFlag,
		ListenPort:           listenPortFlag,
		MaxWitnessSize_bytes: 1024 * 1024,
		Plugins:              plugins,
		StatsLogDelay:        60,
		TelemetryInterval:    300,
	}
	return nginx.RunServer(nginxArgs)
}

func installNginxModule(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("This command is not yet implemented.")
}
