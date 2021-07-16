package apispec

import (
	"fmt"
	"time"

	"github.com/akitasoftware/akita-cli/location"
)

var (
	// Required flags.
	tracesFlag      []string
	tracesByTagFlag []string

	// Optional flags
	outFlag     location.Location
	serviceFlag string

	githubBranchFlag string
	githubCommitFlag string
	githubPRFlag     int
	githubRepoFlag   string

	gitlabProjectFlag string
	gitlabMRFlag      string
	gitlabBranchFlag  string
	gitlabCommitFlag  string

	formatFlag                     string
	tagsFlag                       []string
	versionsFlag                   []string
	getSpecEnableRelatedFieldsFlag bool
	includeTrackersFlag            bool

	fromTimeFlag string
	toTimeFlag   string

	pathParamsFlag     []string
	pathExclusionsFlag []string

	pluginsFlag []string
)

func init() {
	Cmd.Flags().StringSliceVar(
		&tracesFlag,
		"traces",
		nil,
		"The locations to read traces from. Can be a mix of AkitaURI and local file paths.")

	Cmd.Flags().StringSliceVar(
		&tracesByTagFlag,
		"trace-tag",
		nil,
		"Use the most recent trace in the Akita cloud with a matching tag as input.")

	//
	// Optional Flags
	//
	Cmd.Flags().Var(
		&outFlag,
		"out",
		"The location to store the spec. Can be an AkitaURI or a local file. Defaults to a new spec on Akita Cloud.")

	Cmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Akita cloud service to use to generate the spec. Only needed if --out is not an AkitaURI.")

	Cmd.Flags().StringVar(
		&githubBranchFlag,
		"github-branch",
		"",
		"Name of github branch that this spec belongs to. Used to enable GitHub integration.",
	)
	Cmd.Flags().StringVar(
		&githubCommitFlag,
		"github-commit",
		"",
		"SHA of github commit that this spec belongs to. Used to enable GitHub integration.",
	)
	Cmd.Flags().IntVar(
		&githubPRFlag,
		"github-pr",
		0,
		"GitHub PR number that this spec belongs to. Used to enable GitHub integration.",
	)
	Cmd.Flags().StringVar(
		&githubRepoFlag,
		"github-repo",
		"",
		"GitHub repo name of the form <repo_owner>/<repo_name> that this spec belongs to. Used to enable GitHub integration.",
	)

	Cmd.Flags().StringVar(
		&formatFlag,
		"format",
		"yaml",
		"Output format for the specification. Supports 'yaml' and 'json'.",
	)
	Cmd.Flags().StringSliceVar(
		&tagsFlag,
		"tags",
		nil,
		`Adds tags to the spec. Specified as a comma separated list of "key=value" pairs.`,
	)
	Cmd.Flags().StringSliceVar(
		&versionsFlag,
		"versions",
		nil,
		`Assigns versions to the spec.  Versions are similar to tags, but a version may only be assigned to one spec within a service. Specified as a comma separated list of strings.`,
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
		"List of regular expressions used to exclude endpoints from the spec.",
	)

	Cmd.Flags().BoolVar(
		&getSpecEnableRelatedFieldsFlag,
		"infer-field-relations",
		false,
		"If true, enables analysis to determine related fields in your API.",
	)

	Cmd.Flags().BoolVar(
		&includeTrackersFlag,
		"include-trackers",
		false,
		"If set to true, disables automatic filtering of requests to third-party trackers that are recorded in traces.")

	// Figure out the local time zone.
	now := time.Now()
	timeZone, offsetSecs := now.Zone()
	offsetDir := "+"
	if offsetSecs < 0 {
		offsetDir = "-"
		offsetSecs = -offsetSecs
	}
	offsetHours := offsetSecs / 3600
	offsetMinutes := (offsetSecs / 60) % 60

	Cmd.Flags().StringVar(
		&fromTimeFlag,
		"from-time",
		"",
		fmt.Sprintf("If provided, only trace events occurring at or after this time will be used to build the spec. Expected format is 'YYYY-MM-DD hh:mm:ss'. If desired, the 'hh:mm:ss' or the ':ss' can be omitted, in which case the start of the day or minute is used. The client's local time zone is assumed: %s (%s%02d:%02d). If the given time occurs during a transition to or from daylight saving time, then one side of the transition is arbitrarily chosen.", timeZone, offsetDir, offsetHours, offsetMinutes))
	Cmd.Flags().StringVar(
		&toTimeFlag,
		"to-time",
		"",
		fmt.Sprintf("If provided, only trace events occurring at or before this time will be used to build the spec. Expected format is 'YYYY-MM-DD hh:mm:ss'. If desired, the 'hh:mm:ss' or the ':ss' can be omitted, in which case the start of the day or minute is used. The client's local time zone is assumed: %s (%s%02d:%02d). If the given time occurs during a transition to or from daylight saving time, then one side of the transition is arbitrarily chosen.", timeZone, offsetDir, offsetHours, offsetMinutes))

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

	Cmd.Flags().StringSliceVar(
		&pluginsFlag,
		"plugins",
		nil,
		"Paths of third-party Akita plugins. They are executed in the order given.",
	)
	Cmd.Flags().MarkHidden("plugins")
}

func toLocations(traces []string) ([]location.Location, error) {
	locs := make([]location.Location, 0, len(traces))
	for _, t := range traces {
		var loc location.Location
		if err := loc.Set(t); err != nil {
			return nil, err
		}
		locs = append(locs, loc)
	}
	return locs, nil
}
