package apispec

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/gitlab"
	"github.com/akitasoftware/akita-libs/tags"
)

var Cmd = &cobra.Command{
	Use:          "apispec",
	Short:        "Convert traces into an OpenAPI3 specification.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		traces, err := toLocations(tracesFlag)
		if err != nil {
			return err
		}

		tags, err := tags.FromPairs(tagsFlag)
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
			ClientID:       akid.GenerateClientID(),
			Domain:         akiflag.Domain,
			Traces:         traces,
			Out:            outFlag,
			Service:        serviceFlag,
			Format:         formatFlag,
			Tags:           tags,
			PathParams:     pathParamsFlag,
			PathExclusions: pathExclusionsFlag,

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
