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
	"github.com/akitasoftware/akita-cli/cmd/internal/ci_guard"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/daemon"
	"github.com/akitasoftware/akita-cli/cmd/internal/get"
	"github.com/akitasoftware/akita-cli/cmd/internal/learn"
	"github.com/akitasoftware/akita-cli/cmd/internal/legacy"
	"github.com/akitasoftware/akita-cli/cmd/internal/login"
	"github.com/akitasoftware/akita-cli/cmd/internal/man"
	"github.com/akitasoftware/akita-cli/cmd/internal/setversion"
	"github.com/akitasoftware/akita-cli/cmd/internal/upload"
	"github.com/akitasoftware/akita-cli/pcap"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-cli/version"
	"github.com/akitasoftware/akita-libs/akinet/http"
)

var (
	// Test only flags
	testOnlyUseHTTPSFlag                bool
	testOnlyDisableGitHubTeamsCheckFlag bool
	dogfoodFlag                         bool
	debugFlag                           bool
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

	// Semi-secret somewhat-safe flags
	rootCmd.PersistentFlags().Int64Var(&http.MaximumHTTPLength, "max-http-length", 1024*1024, "Maximum size of HTTP body to capture")
	rootCmd.PersistentFlags().MarkHidden("max-http-length")
	viper.BindPFlag("max-http-length", rootCmd.PersistentFlags().Lookup("max-http-length"))

	rootCmd.PersistentFlags().Int64Var(&pcap.StreamTimeoutSeconds, "stream-timeout-seconds", 10, "Maximum time to wait for missing TCP data")
	rootCmd.PersistentFlags().MarkHidden("stream-timeout-seconds")
	viper.BindPFlag("stream-timeout-seconds", rootCmd.PersistentFlags().Lookup("stream-timeout-seconds"))

	// Super secret unsafe test only flags
	rootCmd.PersistentFlags().BoolVar(&testOnlyUseHTTPSFlag, "test_only_disable_https", false, "TEST ONLY - whether to use HTTPS when communicating with backend")
	rootCmd.PersistentFlags().MarkHidden("test_only_disable_https")
	viper.BindPFlag("test_only_disable_https", rootCmd.PersistentFlags().Lookup("test_only_disable_https"))

	rootCmd.PersistentFlags().BoolVar(&dogfoodFlag, "dogfood", false, "Capture HTTP traffic to Akita services that would ordinarily be filtered")
	rootCmd.PersistentFlags().MarkHidden("dogfood")
	viper.BindPFlag("dogfood", rootCmd.PersistentFlags().Lookup("dogfood"))

	rootCmd.PersistentFlags().BoolVar(&testOnlyDisableGitHubTeamsCheckFlag, "test_only_disable_github_teams_check", false, "TEST ONLY - whether to disable the GitHub teams check, even though the environment suggests the CLI is being run as part of CI")
	rootCmd.PersistentFlags().MarkHidden("test_only_disable_github_teams_check")
	viper.BindPFlag("test_only_disable_github_teams_check", rootCmd.PersistentFlags().Lookup("test_only_disable_github_teams_check"))

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

	// Register subcommands. Most commands that interact with Akita Cloud should
	// be guarded by ci_guard.
	rootCmd.AddCommand(ci_guard.GuardCommand(apidiff.Cmd))
	rootCmd.AddCommand(ci_guard.GuardCommand(apidump.Cmd))
	rootCmd.AddCommand(ci_guard.GuardCommand(apispec.Cmd))
	rootCmd.AddCommand(daemon.Cmd)
	rootCmd.AddCommand(ci_guard.GuardCommand(learn.Cmd))
	rootCmd.AddCommand(login.Cmd)
	rootCmd.AddCommand(man.Cmd)
	rootCmd.AddCommand(ci_guard.GuardCommand(setversion.Cmd))
	rootCmd.AddCommand(ci_guard.GuardCommand(upload.Cmd))
	rootCmd.AddCommand(ci_guard.GuardCommand(get.Cmd))

	// Legacy commands, included for backward compatibility but are hidden.
	legacy.SessionsCmd.Hidden = true
	rootCmd.AddCommand(legacy.SessionsCmd)
	legacy.SpecsCmd.Hidden = true
	rootCmd.AddCommand(legacy.SpecsCmd)
}
