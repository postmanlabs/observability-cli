package ci_guard

import (
	"context"
	"time"

	"github.com/akitasoftware/akita-cli/ci"
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-libs/github"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var AkitaGitHubUsersTeamSlug = "akita-users"

// Modifies cmd.RunE in place to do nothing if the CLI is running as part of a
// GitHub PR and the PR is not Akita-enabled. Returns the modified cmd.
func GuardCommand(cmd *cobra.Command) *cobra.Command {
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		_, gitHubPR, _ := ci.GetCIInfo()
		if gitHubPR != nil {
			enabled, err := gitHubPrIsAkitaEnabled(gitHubPR)
			if err != nil {
				return err
			}

			if !enabled {
				printer.Warningf("The GitHub PR %s/%s#%d is not Akita-enabled: the user that opened the PR is not a member of the GitHub team %s/%s. The CLI will now exit without doing anything.", gitHubPR.Repo.Owner, gitHubPR.Repo.Name, gitHubPR.Num, gitHubPR.Repo.Owner, AkitaGitHubUsersTeamSlug)
				return nil
			}
		}

		return cmd.RunE(cmd, args)
	}

	return cmd
}

// Queries Akita Cloud to determine whether the given GitHub PR is
// Akita-enabled.
func gitHubPrIsAkitaEnabled(gitHubPR *github.PullRequest) (bool, error) {
	frontClient := rest.NewFrontClient(akiflag.Domain, akiflag.GetClientID())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	enabled, err := frontClient.GetGitHubPREnabledState(ctx, gitHubPR)
	if err != nil {
		err = errors.Wrap(err, "failed to determine whether GitHub PR is Akita-enabled")
	}
	return enabled, err
}
