package cmd

import (
	goflag "flag"
	httpserv "net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/akitasoftware/akita-cli/cmd/internal/apidump"
	"github.com/akitasoftware/akita-cli/cmd/internal/ascii"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/ec2"
	"github.com/akitasoftware/akita-cli/cmd/internal/ecs"
	"github.com/akitasoftware/akita-cli/cmd/internal/kube"
	"github.com/akitasoftware/akita-cli/cmd/internal/legacy"
	"github.com/akitasoftware/akita-cli/pcap"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
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
	cpuProfile                          string
	cpuProfileOut                       *os.File
	heapProfile                         string
	liveProfileAddress                  string
	noColorFlag                         bool
	logFormatFlag                       string
)

var (
	rootCmd = &cobra.Command{
		Use:           "postman-insights-agent",
		Short:         "The Postman Insights Agent",
		Long:          "Documentation is available at https://learning.postman.com/docs/insights/insights-early-access/",
		Version:       version.CLIDisplayString(),
		SilenceErrors: true, // We print our own errors from subcommands in Execute function
		// Don't print usage after error, we only print help if we cannot parse
		// flags. See init function below.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPreRun:  preRun,
		PersistentPostRun: stopProfiling,
	}
)

func preRun(cmd *cobra.Command, args []string) {
	// Decide on the domain name to use, _before_ initializing telemetry,
	// which may make an API call.
	if rest.Domain == "" {
		rest.Domain = rest.DefaultDomain()
	}

	// Initialize Amplitude-based telemetry of usage information and CLI errors.
	telemetry.Init(true)

	switch logFormatFlag {
	case "json":
		printer.SwitchToJSON()
	case "plain":
		printer.SwitchToPlain()
	case "color", "colour":
		// No change needed
	case "":
		// Default to 'colour'.
	default:
		// Use color
		printer.Warningln("Unknown log format, using `color`.")
	}

	// Emit the version (without hash) at the start of every command.
	// Somehow, this doesn't appear before "postman-insights-agent --version"
	// (good) or "postman-insights-agent --help" (less good), only before
	// commands or the usage information if no command is given.
	printer.Stdout.Infof("Postman Insights Agent %s\n", version.ReleaseVersion())

	// This is after argument parsing so that rest.Domain is correct,
	// but won't be called if there is an error parsing the flags.
	telemetry.CommandLine(cmd.Name(), os.Args)

	startProfiling(cmd, args)
}

func startProfiling(cmd *cobra.Command, args []string) {
	var err error
	if cpuProfile != "" {
		cpuProfileOut, err = os.Create(cpuProfile)
		if err != nil {
			printer.Stderr.Errorf("Can't open CPU profile: %v\n", err)
			os.Exit(1)
		}
		err = pprof.StartCPUProfile(cpuProfileOut)
		if err != nil {
			printer.Stderr.Errorf("Can't start CPU profile: %v\n", err)
			os.Exit(1)
		}
	}

	if liveProfileAddress != "" {
		go func() {
			err := httpserv.ListenAndServe(liveProfileAddress, nil)
			printer.Stderr.Errorf("Profile server error: %v\n", err)
		}()
	}
}

func stopProfiling(cmd *cobra.Command, args []string) {
	if heapProfile != "" {
		f, err := os.Create(heapProfile)
		defer f.Close()
		if err != nil {
			printer.Stderr.Errorf("Can't open heap profile: %v\n", err)
		} else {
			runtime.GC()
			err = pprof.WriteHeapProfile(f)
			if err != nil {
				printer.Stderr.Errorf("Can't write heap profile: %v\n", err)
			}
		}
	}

	if cpuProfileOut != nil {
		pprof.StopCPUProfile()
		cpuProfileOut.Close()
	}
}

func Execute() {
	defer telemetry.Shutdown()

	if cmd, err := rootCmd.ExecuteC(); err != nil {
		if _, isAkitaErr := err.(cmderr.AkitaErr); !isAkitaErr {
			// Print usage for CLI usage errors (e.g. missing arg) but not for akita
			// errors (e.g. failed to find the service).
			cmd.Println(cmd.UsageString())

			// Dump the command line; the call in preRun  will not have been executed.
			telemetry.CommandLine("unknown", os.Args)

		}
		telemetry.Error("command execution", err)

		exitCode := 1
		var exitErr util.ExitError
		if isExitErr := errors.As(err, &exitErr); isExitErr {
			exitCode = exitErr.ExitCode
		}
		printer.Stderr.Errorf("%s\n", err)
		telemetry.Shutdown() // can't call in a defer because we use Exit()
		os.Exit(exitCode)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rest.Domain, "domain", "", "The domain name of the back-end instance to use.")
	rootCmd.PersistentFlags().MarkHidden("domain")

	// Use a proxy or permit a mismatched certificate.
	rootCmd.PersistentFlags().StringVar(&rest.ProxyAddress, "proxy", "", "The domain name, IP address, or URL of an HTTP proxy server to use")
	rootCmd.PersistentFlags().BoolVar(&rest.PermitInvalidCertificate, "skip-tls-validate", false, "Skip TLS validation on the connection to the back end")
	rootCmd.PersistentFlags().MarkHidden("skip-tls-validate")
	rootCmd.PersistentFlags().StringVar(&rest.ExpectedServerName, "server-tls-name", "", "Provide an alternate TLS server name to accept")
	rootCmd.PersistentFlags().MarkHidden("server-tls-name")

	// Semi-secret somewhat-safe flags
	rootCmd.PersistentFlags().Int64Var(&http.MaximumHTTPLength, "max-http-length", 10*1024*1024, "Maximum size of HTTP body to capture")
	rootCmd.PersistentFlags().MarkHidden("max-http-length")
	viper.BindPFlag("max-http-length", rootCmd.PersistentFlags().Lookup("max-http-length"))

	rootCmd.PersistentFlags().Int64Var(&pcap.StreamTimeoutSeconds, "stream-timeout-seconds", 10, "Maximum time to wait for missing TCP data")
	rootCmd.PersistentFlags().MarkHidden("stream-timeout-seconds")
	viper.BindPFlag("stream-timeout-seconds", rootCmd.PersistentFlags().Lookup("stream-timeout-seconds"))

	// For explanation of these defaults see net_parse.go
	rootCmd.PersistentFlags().IntVar(&pcap.MaxBufferedPagesTotal, "gopacket-pages", 150_000, "Maximum number of TCP reassembly pages to allocate per interface")
	rootCmd.PersistentFlags().MarkHidden("gopacket-pages")
	rootCmd.PersistentFlags().IntVar(&pcap.MaxBufferedPagesPerConnection, "gopacket-per-conn", 4_000, "Maximum number of TCP reassembly pages per connection")
	rootCmd.PersistentFlags().MarkHidden("gopacket-per-conn")

	rootCmd.PersistentFlags().StringVar(&liveProfileAddress, "live-profile", "", "Address and port to use for live profiling, 0 to disable")
	rootCmd.PersistentFlags().MarkHidden("live-profile")
	rootCmd.PersistentFlags().StringVar(&cpuProfile, "cpu-profile", "", "File for CPU profile")
	rootCmd.PersistentFlags().MarkHidden("cpu-profile")
	rootCmd.PersistentFlags().StringVar(&heapProfile, "heap-profile", "", "File for heap profile")
	rootCmd.PersistentFlags().MarkHidden("heap-profile")

	// Super secret unsafe test only flags
	rootCmd.PersistentFlags().BoolVar(&testOnlyUseHTTPSFlag, "test_only_disable_https", false, "TEST ONLY - whether to use HTTPS when communicating with backend")
	rootCmd.PersistentFlags().MarkHidden("test_only_disable_https")
	viper.BindPFlag("test_only_disable_https", rootCmd.PersistentFlags().Lookup("test_only_disable_https"))

	rootCmd.PersistentFlags().BoolVar(&dogfoodFlag, "dogfood", false, "Capture HTTP traffic to Postman services that would ordinarily be filtered, and enable assertions")
	rootCmd.PersistentFlags().MarkHidden("dogfood")
	viper.BindPFlag("dogfood", rootCmd.PersistentFlags().Lookup("dogfood"))

	rootCmd.PersistentFlags().BoolVar(&testOnlyDisableGitHubTeamsCheckFlag, "test_only_disable_github_teams_check", false, "TEST ONLY - whether to disable the GitHub teams check, even though the environment suggests the CLI is being run as part of CI")
	rootCmd.PersistentFlags().MarkHidden("test_only_disable_github_teams_check")
	viper.BindPFlag("test_only_disable_github_teams_check", rootCmd.PersistentFlags().Lookup("test_only_disable_github_teams_check"))

	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "If set, outputs detailed information for debugging.")
	rootCmd.PersistentFlags().MarkHidden("debug")
	viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))

	rootCmd.PersistentFlags().StringVar(&logFormatFlag, "log-format", "", "Set to 'color', 'plain' or 'json' to control the log format.")

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

	rootCmd.AddCommand(apidump.Cmd)

	rootCmd.AddCommand(ecs.Cmd)
	rootCmd.AddCommand(kube.Cmd)
	rootCmd.AddCommand(ec2.Cmd)

	// Easter egg.
	rootCmd.AddCommand(ascii.Cmd)

	// Legacy command, included for integration tests but is hidden.
	legacy.SpecsCmd.Hidden = true
	rootCmd.AddCommand(legacy.SpecsCmd)
}
