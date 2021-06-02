package cmd

import (
	goflag "flag"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/apidiff"
	"github.com/akitasoftware/akita-cli/cmd/internal/apidump"
	"github.com/akitasoftware/akita-cli/cmd/internal/apispec"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/daemon"
	"github.com/akitasoftware/akita-cli/cmd/internal/learn"
	"github.com/akitasoftware/akita-cli/cmd/internal/legacy"
	"github.com/akitasoftware/akita-cli/cmd/internal/login"
	"github.com/akitasoftware/akita-cli/cmd/internal/man"
	"github.com/akitasoftware/akita-cli/cmd/internal/setversion"
	"github.com/akitasoftware/akita-cli/cmd/internal/upload"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-cli/version"
)

var (
	// Test only flags
	testOnlyUseHTTPSFlag bool
	dogfoodFlag          bool
	debugFlag            bool
)

const (
	defaultDomain = "akita.software"
)

var (
	rootCmd = &cobra.Command{
		Use:           "akita",
		Short:         "Gateway to all Akita services.",
		Long:          "Complete documentation is available at https://docs.akita.software",
		Version:       version.CLIDisplayString(),
		SilenceErrors: true, // We print our own errors from subcommands in Execute function
		// Don't print usage after error, we only print help if we cannot parse
		// flags. See init function below.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
)

func Execute() {
	if cmd, err := rootCmd.ExecuteC(); err != nil {
		if _, isAkitaErr := err.(cmderr.AkitaErr); !isAkitaErr {
			// Print usage for CLI usage errors (e.g. missing arg) but not for akita
			// errors (e.g. failed to find the service).
			cmd.Println(cmd.UsageString())
		}

		exitCode := 1
		var exitErr util.ExitError
		if isExitErr := errors.As(err, &exitErr); isExitErr {
			exitCode = exitErr.ExitCode
		}
		printer.Stderr.Errorf("%s\n", err)
		os.Exit(exitCode)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&akiflag.Domain, "domain", defaultDomain, "Your assigned Akita domain (e.g. company.akita.software)")
	rootCmd.PersistentFlags().MarkHidden("domain")

	// Super secret unsafe test only flags
	rootCmd.PersistentFlags().BoolVar(&testOnlyUseHTTPSFlag, "test_only_disable_https", false, "TEST ONLY - whether to use HTTPS when communicating with backend")
	rootCmd.PersistentFlags().MarkHidden("test_only_disable_https")
	viper.BindPFlag("test_only_disable_https", rootCmd.PersistentFlags().Lookup("test_only_disable_https"))

	rootCmd.PersistentFlags().BoolVar(&dogfoodFlag, "dogfood", false, "TEST ONLY - whether to enable dogfooding")
	rootCmd.PersistentFlags().MarkHidden("dogfood")
	viper.BindPFlag("dogfood", rootCmd.PersistentFlags().Lookup("dogfood"))

	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "If set, outputs detailed information for debugging.")
	rootCmd.PersistentFlags().MarkHidden("debug")
	viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))

	// Include flags from go libraries that we're using. We hand-pick the flags to
	// include to avoid polluting the flag set of the CLI.
	goflag.CommandLine.VisitAll(func(f *goflag.Flag) {
		includeFlag := false
		switch f.Name {
		case "alsologtostderr", "log_dir", "logtostderr", "v":
			// Select glog flags to include.
			includeFlag = true
		}
		if includeFlag {
			flag.CommandLine.AddGoFlag(f)
			flag.CommandLine.MarkHidden(f.Name)
		}
	})

	// Handle custom glog flag setup.
	// The CLI currently shares some libraries with the backend, so we can't
	// completely remove glog as a dependency yet. Most user-visible error
	// messages should be handled by the printer package, and we let the few
	// messages from glog to go to stderr only.
	{
		// Call Parse with empty args so the go flag library thinks it has parsed the
		// flags, when in reality only the selected flags will get parsed by
		// pflag/cobra. This is needed for glog library to stop complaining that flags
		// have not been parsed.
		goflag.CommandLine.Parse(nil)

		// Disable glog logging to file so the CLI doesn't create log files in the
		// user's temp directory.
		flag.CommandLine.Set("logtostderr", "true")

		// Share verbose logging flag with glog.
		viper.BindPFlag("verbose-level", flag.CommandLine.Lookup("v"))
	}

	// Register subcommands.
	rootCmd.AddCommand(apidiff.Cmd)
	rootCmd.AddCommand(apidump.Cmd)
	rootCmd.AddCommand(apispec.Cmd)
	rootCmd.AddCommand(daemon.Cmd)
	rootCmd.AddCommand(learn.Cmd)
	rootCmd.AddCommand(login.Cmd)
	rootCmd.AddCommand(man.Cmd)
	rootCmd.AddCommand(setversion.Cmd)
	rootCmd.AddCommand(upload.Cmd)

	// Legacy commands, included for backward compatibility but are hidden.
	legacy.SessionsCmd.Hidden = true
	rootCmd.AddCommand(legacy.SessionsCmd)
	legacy.SpecsCmd.Hidden = true
	rootCmd.AddCommand(legacy.SpecsCmd)
}
