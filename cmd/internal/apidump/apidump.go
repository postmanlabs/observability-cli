package apidump

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/apidump"
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-cli/location"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akiuri"
)

var (
	// Optional flags
	outFlag             location.Location
	serviceFlag         string
	interfacesFlag      []string
	filterFlag          string
	sampleRateFlag      float64
	rateLimitFlag       float64
	tagsFlag            []string
	appendByTagFlag     bool
	pathExclusionsFlag  []string
	hostExclusionsFlag  []string
	pathAllowlistFlag   []string
	hostAllowlistFlag   []string
	execCommandFlag     string
	execCommandUserFlag string
	pluginsFlag         []string
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

		// Check that exactly one of --out or --service is specified.
		if outFlag.IsSet() == (serviceFlag != "") {
			return errors.New("exactly one of --out or --service must be specified")
		}

		// If --service was given, convert it to an equivalent --out.
		if serviceFlag != "" {
			uri, err := akiuri.Parse(akiuri.Scheme + serviceFlag)
			if err != nil {
				return errors.Wrap(err, "bad service name")
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

		args := apidump.Args{
			ClientID:           akiflag.GetClientID(),
			Domain:             akiflag.Domain,
			Out:                outFlag,
			Tags:               traceTags,
			SampleRate:         sampleRateFlag,
			WitnessesPerMinute: rateLimitFlag,
			Interfaces:         interfacesFlag,
			Filter:             filterFlag,
			PathExclusions:     pathExclusionsFlag,
			HostExclusions:     hostExclusionsFlag,
			PathAllowlist:      pathAllowlistFlag,
			HostAllowlist:      hostAllowlistFlag,
			ExecCommand:        execCommandFlag,
			ExecCommandUser:    execCommandUserFlag,
			Plugins:            plugins,
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
		"The location to store the trace. Can be an AkitaURI or a local directory. Defaults to a trace on the Akita Cloud. Exactly one of --out, --cluster, or --service must be specified.")

	Cmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Your Akita service. Exactly one of --out, --cluster, or --service must be specified.")

	Cmd.Flags().StringVar(
		&serviceFlag,
		"cluster",
		"",
		"Your Akita cluster (alias for 'service'). Exactly one of --out, --cluster, or --service must be specified.")

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
		0.0,
		"Number of requests per minute to capture. Defaults to unlimited.",
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
}
