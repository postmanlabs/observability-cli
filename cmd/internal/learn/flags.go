package learn

import (
	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/location"
)

var (
	// Optional flags
	serviceFlag    string
	outFlag        location.Location
	filterFlag     string
	interfacesFlag []string
	tagsFlag       []string
	sampleRateFlag float64
	rateLimitFlag  float64

	pathExclusionsFlag []string
	hostExclusionsFlag []string
	pathAllowlistFlag  []string
	hostAllowlistFlag  []string
	statsLogDelay      int

	execCommandFlag     string
	execCommandUserFlag string

	pluginsFlag []string

	// Hidden legacy flags to preserve compatibility with old CLI.
	legacyBPFFlag       string
	legacyPortFlag      uint16
	legacySessionFlag   string
	legacyHARFlag       bool
	legacyHARDirFlag    string
	legacyHARSampleFlag float64
)

func init() {
	registerRequiredFlags()
	registerHiddenLegacyFlags()
	registerOptionalFlags()
}

func registerRequiredFlags() {
}

func registerOptionalFlags() {
	Cmd.Flags().StringVar(
		&serviceFlag,
		"project",
		"",
		"Your Akita project.")

	Cmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Your Akita project.")
	Cmd.Flags().MarkDeprecated("service", "use --project instead.")

	Cmd.Flags().StringVar(
		&serviceFlag,
		"cluster",
		"",
		"Your Akita project.")
	Cmd.Flags().MarkDeprecated("cluster", "use --project instead.")

	Cmd.Flags().Var(
		&outFlag,
		"out",
		"An AkitaURI or a file. Used to derive your Akita project, and is ignored if it names a file.")
	Cmd.Flags().MarkDeprecated("out", "use --project instead.")

	Cmd.Flags().StringVar(
		&filterFlag,
		"filter",
		"",
		"Used to match packets going to and coming from your API service.")

	Cmd.Flags().StringSliceVar(
		&tagsFlag,
		"tags",
		nil,
		`Adds tags to the new learn session. Specified as a comma separated list of "key=value" pairs.

Ignored if --session is used to attach to an existing learn session.
`,
	)

	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"versions"},
		`Assigns versions to the spec.  Versions are similar to tags, but a version may only be assigned to one spec within a project. Specified as a comma separated list of strings.

Ignored if --session is used to attach to an existing learn session.
`,
	)

	// Old version of CLI uses --interface, whereas new version uses --interfaces
	// (note plural). Define both flags here and hide the old version.
	akiflag.RenameStringSliceFlag(
		Cmd.Flags(),
		&interfacesFlag,
		"interface",
		"interfaces",
		nil,
		`List of network interfaces to listen on (e.g. "lo" or "eth0").

If not set, defaults to all interfaces on the host.

You may specify multiple interfaces by using a comma-separated list (e.g.
"--interface 'lo, eth0'") or specifying this flag multiple times (e.g.
"--interface lo --interface eth0").
`,
	)

	Cmd.Flags().Float64Var(
		&sampleRateFlag,
		"sample-rate",
		1.0,
		"A number between [0.0, 1.0] to control sampling.",
	)
	Cmd.Flags().MarkDeprecated("sample-rate", "use --rate-limit instead.")

	Cmd.Flags().Float64Var(
		&rateLimitFlag,
		"rate-limit",
		apispec.DefaultRateLimit,
		"Number of requests per minute to capture.",
	)

	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"path-parameters"},
		"List of patterns used to override endpoint paths that have been automatically inferred by Akita. See akita man apispec for more details.",
	)

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

	Cmd.Flags().IntVar(
		&statsLogDelay,
		"stats-log-delay",
		apispec.DefaultStatsLogDelay_seconds,
		"Print packet capture statistics after N seconds.",
	)
	// GitHub integration flags.
	// Both underscore and dash versions are supported to support legacy behavior
	// that uses underscore. Exception is --github-repo, which replaces
	// --github_repo_url.
	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"github-repo"},
		"GitHub repo name of the form <repo_owner>/<repo_name> that this spec belongs to. Used to enable GitHub integration.",
	)

	akiflag.IgnoreIntFlags(
		Cmd.Flags(),
		[]string{"github_pr", "github-pr"},
		"GitHub PR number.",
	)

	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"github_commit", "github-commit"},
		"Commit SHA for the GitHub commit under test.",
	)

	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"github_branch", "github-branch"},
		"Name of the the GitHub branch under test.",
	)

	// GitLab integration
	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"gitlab-project"},
		"Gitlab project ID or URL-encoded path",
	)

	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"gitlab-mr"},
		"GitLab merge request IID",
	)

	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"gitlab-branch"},
		"Name of gitlab branch that this spec belongs to.",
	)

	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"gitlab-commit"},
		"SHA of gitlab commit that this spec belongs to.",
	)

	// Hidden optional flags
	akiflag.IgnoreDurationFlags(
		Cmd.Flags(),
		[]string{"checkpoint_timeout"},
		`Timeout for creating a checkpoint.`,
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

func registerHiddenLegacyFlags() {
	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"extend"},
		`An API spec ID or version name for the API spec to expand on.

If specified, Akita will add learnings about your API from this run into the API
spec specified, allowing you to improve your API spec incrementally.

Use "latest" to specify the most recently created API spec.
`,
	)

	Cmd.Flags().StringVar(
		&legacySessionFlag,
		"session",
		"",
		`Attach to an existing learn session by ID.

If specified, Akita attaches learnings about your API from this run to the
specified session, allowing you to run Akita in parallel across multiple
machines while generating a combined API spec.

Use "learn-sessions list" command to get the list of learn session IDs.
`,
	)
	Cmd.Flags().MarkHidden("session")

	// --bpf-filter and --port are replaced by --filter
	Cmd.Flags().StringVar(
		&legacyBPFFlag,
		"bpf-filter",
		"",
		`BPF filter to use when capturing packets.

Set this to match your API's traffic to identify packets going to and coming
from your API service.

This filter is applied uniformly across all network interfaces. You may wish to
customize the filter for each interface by filtering on attributes such as IP or
MAC address.

May not be used in conjunction with --port.
`)
	Cmd.Flags().MarkHidden("bpf-filter")

	Cmd.Flags().Uint16Var(
		&legacyPortFlag,
		"port",
		0,
		`Filter captured packets by port.

Set this to your API's port to identify packets going to and coming from your
API service.

This is equivalent to setting --bpf-filter="port {PORT}" and may not be used in
conjunction with --bpf-filter.
`)
	Cmd.Flags().MarkHidden("port")

	// HAR output
	Cmd.Flags().BoolVar(
		&legacyHARFlag,
		"har",
		false,
		"If true, generates HAR files from witnesses and writes them locally.",
	)
	Cmd.Flags().MarkHidden("har")
	Cmd.Flags().StringVar(
		&legacyHARDirFlag,
		"har_output_dir",
		".",
		"Where to write HAR files. Only applicable if -har=true.",
	)
	Cmd.Flags().MarkHidden("har_output_dir")
	Cmd.Flags().Float64Var(
		&legacyHARSampleFlag,
		"har_sample_rate",
		1.0,
		"A number between [0.0, 1.0] to control sampling of HAR output. Only applicable if --har=true.",
	)
	Cmd.Flags().MarkHidden("har_sample_rate")

	// GitHub integration
	akiflag.IgnoreStringFlags(
		Cmd.Flags(),
		[]string{"github_repo_url"},
		"URL of the GitHub repo under test. Use this to set up GitHub integration manually.",
	)
}
