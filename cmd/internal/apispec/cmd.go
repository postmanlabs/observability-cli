package apispec

import (
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-cli/location"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/gitlab"
	"github.com/akitasoftware/akita-libs/time_span"
	"github.com/akitasoftware/akita-libs/version_names"
)

func parseTime(s string) (time.Time, error) {
	result, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err == nil {
		return result, nil
	}

	result, err = time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err == nil {
		return result, nil
	}

	return time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
}

var Cmd = &cobra.Command{
	Deprecated:   "API specs are created automatically in the Akita app.",
	Use:          "apispec",
	Short:        "Convert traces into an OpenAPI3 specification.",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		traces, err := toLocations(tracesFlag)
		if err != nil {
			return err
		}

		// No need to warn here, matching on reserved tags is OK
		traceTags, err := util.ParseTags(tracesByTagFlag)
		if err != nil {
			return err
		}

		// Check for reserved versions.
		for _, version := range versionsFlag {
			if version_names.IsReservedVersionName(version) {
				return errors.Errorf("'%s' is an Akita-reserved version", version)
			}
		}

		var timeRange *time_span.TimeSpan
		var startTime time.Time
		endTime := time.Now()
		if fromTimeFlag != "" {
			startTime, err = parseTime(fromTimeFlag)
			if err != nil {
				return errors.Wrap(err, "failed to parse start time")
			}
		}
		if toTimeFlag != "" {
			endTime, err = parseTime(toTimeFlag)
			if err != nil {
				return errors.Wrap(err, "failed to parse end time")
			}
		}
		if fromTimeFlag != "" || toTimeFlag != "" {
			timeRange = time_span.NewTimeSpan(startTime, endTime)
		}

		if len(traces) == 0 && len(traceTags) == 0 {
			return errors.New("Must specify at least one input via \"traces\" or \"trace-tag\"")
		}

		if len(traceTags) > 0 {
			var projectName string
			if serviceFlag != "" {
				projectName = serviceFlag
			} else if outFlag.AkitaURI != nil {
				projectName = outFlag.AkitaURI.ServiceName
			} else {
				return errors.New("Must specify \"project\" or \"out\" to use \"trace-tag\"")
			}
			destURI, err := util.GetTraceURIByTags(rest.Domain, telemetry.GetClientID(), projectName, traceTags, "trace-tag")
			if err != nil {
				return err
			}
			if destURI.ObjectName == "" {
				return cmderr.AkitaErr{Err: errors.New("No traces matching specified tag, cannot create spec")}
			}
			traces = append(traces, location.Location{AkitaURI: &destURI})
		}

		tags, err := util.ParseTagsAndWarn(tagsFlag)
		if err != nil {
			return err
		}

		var gitlabMR *gitlab.MRInfo
		if gitlabProjectFlag != "" {
			gitlabMR = &gitlab.MRInfo{
				Project: gitlabProjectFlag,
				IID:     gitlabMRFlag,
				Branch:  gitlabBranchFlag,
				Commit:  gitlabCommitFlag,
			}
		}

		plugins, err := pluginloader.Load(pluginsFlag)
		if err != nil {
			return errors.Wrap(err, "failed to load plugins")
		}

		args := apispec.Args{
			ClientID:       telemetry.GetClientID(),
			Domain:         rest.Domain,
			Traces:         traces,
			Out:            outFlag,
			Service:        serviceFlag,
			Format:         formatFlag,
			Tags:           tags,
			Versions:       versionsFlag,
			PathParams:     pathParamsFlag,
			PathExclusions: pathExclusionsFlag,
			TimeRange:      timeRange,

			GitHubBranch: githubBranchFlag,
			GitHubCommit: githubCommitFlag,
			GitHubRepo:   githubRepoFlag,
			GitHubPR:     githubPRFlag,

			GitLabMR: gitlabMR,

			GetSpecEnableRelatedFields: getSpecEnableRelatedFieldsFlag,
			IncludeTrackers:            includeTrackersFlag,

			Plugins: plugins,
		}
		if err := apispec.Run(args); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}
