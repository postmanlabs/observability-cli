package nginx

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-cli/consts"
	"github.com/akitasoftware/akita-cli/integrations/nginx"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
)

var (
	// Akita project name
	projectFlag string

	// Port number that the module will send traffic to
	listenPortFlag uint16

	// Development mode -- dump out traffic locally
	developmentFlag bool

	// Dry run for install -- find version but do not install
	dryRunFlag bool

	// Module destination
	moduleDestFlag string
)

var Cmd = &cobra.Command{
	Deprecated:   "This is no longer supported and will be removed in a future release.",
	Use:          "nginx",
	Short:        "Install or use Akita's NGINX module to collect API traffic.",
	SilenceUsage: true,
}

var CaptureCmd = &cobra.Command{
	Use:          "capture",
	Short:        "Capture traffic forwarded from Akita's NGINX module.",
	Long:         "Open a network port for communication with the Akita NGINX module. The agent will parse incoming traffic, obfuscate it, and forward it to the Akita Cloud.",
	SilenceUsage: true,
	RunE:         captureNginxTraffic,
}

var InstallCmd = &cobra.Command{
	Use:          "install",
	Short:        "Download a precompiled NGINX module.",
	Long:         "Download a precompiled version of akita-nginx-module that matches the currently installed version of NGINX.",
	SilenceUsage: true,
	RunE:         installNginxModule,
}

func init() {
	CaptureCmd.PersistentFlags().StringVar(&projectFlag, "project", "", "Your Akita project.")
	CaptureCmd.PersistentFlags().Uint16Var(&listenPortFlag, "port", 50080, "The port number on which to listen for connections.")
	CaptureCmd.PersistentFlags().BoolVar(&developmentFlag, "dev", false, "Enable development mode; only dumps traffic.")
	CaptureCmd.PersistentFlags().MarkHidden("dev")

	Cmd.AddCommand(CaptureCmd)

	InstallCmd.PersistentFlags().BoolVar(&dryRunFlag, "dry-run", false, "Determine NGINX version but do not download or install the module.")
	InstallCmd.PersistentFlags().StringVar(&moduleDestFlag, "dest", "", "Specify the directory into which to install the module.")
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
	err := nginx.InstallModule(&nginx.InstallArgs{
		DryRun: dryRunFlag,
	})
	if err != nil {
		var installError *nginx.InstallationError
		switch {
		case errors.As(err, &installError):
			// Log the error, then what the user should do next
			printer.Errorf("%v\n", err)
			printer.Infof("%v\n", installError.Remedy)
		default:
			printer.Errorf("Could not determine which NGINX platform and version to support: %v\n", err)
			printer.Infof("Please contact %s for assistance, or follow the instructions at https://github.com/akitasoftware/akita-nginx-module to install the module by hand.\n", consts.SupportEmail)
		}

		// Report the error here because we don't report it to the root command
		telemetry.Error("command execution", err)

	}
	return nil
}
