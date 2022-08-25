package apidump

import (
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/apidump"
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-cli/location"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akiuri"
)

var (
	// Optional flags
	outFlag                 location.Location
	serviceFlag             string
	interfacesFlag          []string
	filterFlag              string
	sampleRateFlag          float64
	rateLimitFlag           float64
	tagsFlag                []string
	appendByTagFlag         bool
	pathExclusionsFlag      []string
	hostExclusionsFlag      []string
	pathAllowlistFlag       []string
	hostAllowlistFlag       []string
	execCommandFlag         string
	execCommandUserFlag     string
	pluginsFlag             []string
	traceRotateFlag         string
	deploymentFlag          string
	statsLogDelay           int
	telemetryInterval       int
	collectTCPAndTLSReports bool
)

var Cmd = &cobra.Command{
	Use:          "apidump",
	Short:        "Capture requests/responses from network traffic.",
	Long:         "Capture and store a sequence of requests/responses to a service by observing network traffic.",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		traceTags, err := util.ParseTagsAndWarn(tagsFlag)
		if err != nil {
			return err
		}

		plugins, err := pluginloader.Load(pluginsFlag)
		if err != nil {
			return errors.Wrap(err, "failed to load plugins")
		}

		// Check that exactly one of --out or --project is specified.
		if outFlag.IsSet() == (serviceFlag != "") {
			return errors.New("exactly one of --out or --project must be specified")
		}

		// If --project was given, convert it to an equivalent --out.
		if serviceFlag != "" {
			uri, err := akiuri.Parse(akiuri.Scheme + serviceFlag)
			if err != nil {
				return errors.Wrap(err, "bad project name")
			}
			outFlag.AkitaURI = &uri
		}

		// Look up existing trace by tags
		if appendByTagFlag {
			if outFlag.AkitaURI == nil {
				return errors.New("\"append-by-tag\" can only be used with a cloud-based trace")
			}

			if outFlag.AkitaURI.ObjectName != "" {
				return errors.New("Cannot specify a trace name together with \"append-by-tag\"")
			}

			destURI, err := util.GetTraceURIByTags(akiflag.Domain,
				akiflag.GetClientID(),
				outFlag.AkitaURI.ServiceName,
				traceTags,
				"append-by-tag",
			)
			if err != nil {
				return err
			}
			if destURI.ObjectName != "" {
				outFlag.AkitaURI = &destURI
			}
		}

		// Allow specification of an alternate rotation time, default 1h.
		// But, if the trace name is explicitly given, or selected by tag,
		// or we're sending the output to a local file, then we cannot rotate.
		traceRotateInterval := time.Duration(0)
		if outFlag.AkitaURI != nil {
			if traceRotateFlag != "" {
				if outFlag.AkitaURI.ObjectName != "" {
					return errors.New("Cannot specify trace rotation along with a specific trace.")
				}
				traceRotateInterval, err = time.ParseDuration(traceRotateFlag)
				if err != nil {
					return errors.Wrap(err, "Failed to parse trace rotation interval.")
				}
			} else {
				if outFlag.AkitaURI.ObjectName == "" {
					traceRotateInterval = time.Hour
				}
			}
		}

		if deploymentFlag == "" {
			deploymentFlag = "default"
			if os.Getenv("AKITA_DEPLOYMENT") != "" {
				deploymentFlag = os.Getenv("AKITA_DEPLOYMENT")
			}
		} else if deploymentFlag == "-" {
			// Undocumented feature to disable setting the flag, since
			// we can't re-use "" for this.
			deploymentFlag = ""
		} else {
			if os.Getenv("AKITA_DEPLOYMENT") != "" && os.Getenv("AKITA_DEPLOYMENT") != deploymentFlag {
				printer.Stderr.Warningf("Deployment in environment variable %q overridden by the command line value %q.",
					os.Getenv("AKITA_DEPLOYMENT"),
					deploymentFlag,
				)
			}
		}

		// Rate limit must be greater than zero.
		if rateLimitFlag <= 0.0 {
			rateLimitFlag = 1000.0
		}

		args := apidump.Args{
			ClientID:                akiflag.GetClientID(),
			Domain:                  akiflag.Domain,
			Out:                     outFlag,
			Tags:                    traceTags,
			SampleRate:              sampleRateFlag,
			WitnessesPerMinute:      rateLimitFlag,
			Interfaces:              interfacesFlag,
			Filter:                  filterFlag,
			PathExclusions:          pathExclusionsFlag,
			HostExclusions:          hostExclusionsFlag,
			PathAllowlist:           pathAllowlistFlag,
			HostAllowlist:           hostAllowlistFlag,
			ExecCommand:             execCommandFlag,
			ExecCommandUser:         execCommandUserFlag,
			Plugins:                 plugins,
			LearnSessionLifetime:    traceRotateInterval,
			Deployment:              deploymentFlag,
			StatsLogDelay:           statsLogDelay,
			TelemetryInterval:       telemetryInterval,
			CollectTCPAndTLSReports: collectTCPAndTLSReports,
		}
		if err := apidump.Run(args); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}

func init() {
	Cmd.Flags().Var(
		&outFlag,
		"out",
		"The location to store the trace. Can be an AkitaURI or a local directory. Defaults to a trace on the Akita Cloud. Exactly one of --out or --project must be specified.")

	Cmd.Flags().StringVar(
		&serviceFlag,
		"project",
		"",
		"Your Akita project. Exactly one of --out or --project must be specified.")

	Cmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Your Akita project. DEPRECATED, prefer --project.")

	Cmd.Flags().StringVar(
		&serviceFlag,
		"cluster",
		"",
		"Your Akita project. DEPRECATED, prefer --project.")

	Cmd.Flags().StringVar(
		&filterFlag,
		"filter",
		"",
		"Used to match packets going to and coming from your API service.")

	Cmd.Flags().StringSliceVar(
		&interfacesFlag,
		"interfaces",
		nil,
		"List of network interfaces to listen on. Defaults to all interfaces on host.")

	Cmd.Flags().Float64Var(
		&sampleRateFlag,
		"sample-rate",
		1.0,
		"A number between [0.0, 1.0] to control sampling. DEPRECATED, prefer --rate-limit.",
	)

	Cmd.Flags().Float64Var(
		&rateLimitFlag,
		"rate-limit",
		1000.0,
		"Number of requests per minute to capture.",
	)

	Cmd.Flags().StringSliceVar(
		&tagsFlag,
		"tags",
		nil,
		`Adds tags to the dump. Specified as a comma separated list of "key=value" pairs.`,
	)

	Cmd.Flags().BoolVar(
		&appendByTagFlag,
		"append-by-tag",
		false,
		"Add to the most recent Akita trace with matching tag.")

	Cmd.Flags().StringSliceVar(
		&pathExclusionsFlag,
		"path-exclusions",
		nil,
		"Removes HTTP paths matching regular expressions.",
	)

	Cmd.Flags().StringSliceVar(
		&hostExclusionsFlag,
		"host-exclusions",
		nil,
		"Removes HTTP hosts matching regular expressions.",
	)

	Cmd.Flags().StringSliceVar(
		&pathAllowlistFlag,
		"path-allow",
		nil,
		"Allows only HTTP paths matching regular expressions.",
	)

	Cmd.Flags().StringSliceVar(
		&hostAllowlistFlag,
		"host-allow",
		nil,
		"Allows only HTTP hosts matching regular expressions.",
	)

	Cmd.Flags().StringVarP(
		&execCommandFlag,
		"command",
		"c",
		"",
		"Command to generate API traffic.",
	)

	Cmd.Flags().StringVarP(
		&execCommandUserFlag,
		"user",
		"u",
		"",
		"User to use when running command specified by -c. Defaults to current user.",
	)

	Cmd.Flags().StringSliceVar(
		&pluginsFlag,
		"plugins",
		nil,
		"Paths of third-party Akita plugins. They are executed in the order given.",
	)
	Cmd.Flags().MarkHidden("plugins")

	Cmd.Flags().StringVar(
		&traceRotateFlag,
		"trace-rotate",
		"",
		"Interval at which the trace will be rotated to a new learn session.",
	)
	Cmd.Flags().MarkHidden("trace-rotate")

	Cmd.Flags().StringVar(
		&deploymentFlag,
		"deployment",
		"",
		"Deployment name to use.  DEPRECATED, prefer creating separate projects for different deployment environments, e.g. 'my-project-prod' and 'my-project-staging'.",
	)

	Cmd.Flags().IntVar(
		&statsLogDelay,
		"stats-log-delay",
		60,
		"Print packet capture statistics after N seconds.",
	)

	Cmd.Flags().IntVar(
		&telemetryInterval,
		"telemetry-interval",
		5*60, // 5 minutes
		"Upload client telemetry every N seconds.",
	)
	Cmd.Flags().MarkHidden("telemetry-interval")

	Cmd.Flags().BoolVar(
		&collectTCPAndTLSReports,
		"report-tcp-and-tls",
		false,
		"Collect TCP and TLS reports.",
	)
	Cmd.Flags().MarkHidden("report-tcp-and-tls")
}
