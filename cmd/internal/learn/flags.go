package learn

import (
	"time"

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
	versionsFlag   []string
	sampleRateFlag float64
	rateLimitFlag  float64

	pathParamsFlag     []string
	pathExclusionsFlag []string
	hostExclusionsFlag []string
	pathAllowlistFlag  []string
	hostAllowlistFlag  []string

	execCommandFlag     string
	execCommandUserFlag string

	pluginsFlag []string

	checkpointTimeoutFlag time.Duration

	// GitHub integration
	githubBranchFlag string
	githubCommitFlag string
	githubPRFlag     int
	githubRepoFlag   string

	// GitLab integration
	gitlabProjectFlag string
	gitlabMRFlag      string
	gitlabBranchFlag  string
	gitlabCommitFlag  string

	// Hidden legacy flags to preserve compatibility with old CLI.
	legacyBPFFlag       string
	legacyPortFlag      uint16
	legacyExtendFlag    string
	legacySessionFlag   string
	legacyHARFlag       bool
	legacyHARDirFlag    string
	legacyHARSampleFlag float64
	legacyGitHubURLFlag string
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
		"service",
		"",
		"Akita cloud service to use to generate the spec. Only needed if --out is not an AkitaURI.")

	Cmd.Flags().Var(
		&outFlag,
		"out",
		"The location to store the spec. Can be an AkitaURI or a local file. Defaults to Akita Cloud.")

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

	Cmd.Flags().StringSliceVar(
		&versionsFlag,
		"versions",
		nil,
		`Assigns versions to the spec.  Versions are similar to tags, but a version may only be assigned to one spec within a service. Specified as a comma separated list of strings.

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
`)

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
		&pathParamsFlag,
		"path-parameters",
		nil,
		"List of patterns used to override endpoint paths that have been automatically inferred by Akita. See akita man apispec for more details.")

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

	// GitHub integration flags.
	// Both underscore and dash versions are supported to support legacy behavior
	// that uses underscore. Exception is --github-repo, which replaces
	// --github_repo_url.
	Cmd.Flags().StringVar(
		&githubRepoFlag,
		"github-repo",
		"",
		"GitHub repo name of the form <repo_owner>/<repo_name> that this spec belongs to. Used to enable GitHub integration.",
	)
	akiflag.RenameIntFlag(
		Cmd.Flags(),
		&githubPRFlag,
		"github_pr",
		"github-pr",
		0,
		"GitHub PR number.",
	)
	akiflag.RenameStringFlag(
		Cmd.Flags(),
		&githubCommitFlag,
		"github_commit",
		"github-commit",
		"",
		"Commit SHA for the GitHub commit under test.",
	)
	akiflag.RenameStringFlag(
		Cmd.Flags(),
		&githubBranchFlag,
		"github_branch",
		"github-branch",
		"",
		"Name of the the GitHub branch under test.",
	)

	// GitLab integration
	Cmd.Flags().StringVar(
		&gitlabProjectFlag,
		"gitlab-project",
		"",
		"Gitlab project ID or URL-encoded path")

	Cmd.Flags().StringVar(
		&gitlabMRFlag,
		"gitlab-mr",
		"",
		"GitLab merge request IID")

	Cmd.Flags().StringVar(
		&gitlabBranchFlag,
		"gitlab-branch",
		"",
		"Name of gitlab branch that this spec belongs to.",
	)

	Cmd.Flags().StringVar(
		&gitlabCommitFlag,
		"gitlab-commit",
		"",
		"SHA of gitlab commit that this spec belongs to.",
	)

	// Hidden optional flags
	Cmd.Flags().DurationVar(
		&checkpointTimeoutFlag,
		"checkpoint_timeout",
		60*time.Second, // matches ALB gateway timeout
		`Timeout for creating a checkpoint.`,
	)
	Cmd.Flags().MarkHidden("checkpoint_timeout")

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
	Cmd.Flags().StringVar(
		&legacyExtendFlag,
		"extend",
		"",
		`An API spec ID or version name for the API spec to expand on.

If specified, Akita will add learnings about your API from this run into the API
spec specified, allowing you to improve your API spec incrementally.

Use "latest" to specify the most recently created API spec.
`,
	)
	Cmd.Flags().MarkHidden("extend")

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
	Cmd.Flags().StringVar(
		&legacyGitHubURLFlag,
		"github_repo_url",
		"",
		"URL of the GitHub repo under test. Use this to set up GitHub integration manually.",
	)
	Cmd.Flags().MarkHidden("github_repo_url")
}
