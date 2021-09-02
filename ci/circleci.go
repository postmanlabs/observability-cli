package ci

import (
	"os"

	"github.com/akitasoftware/akita-libs/github"
	"github.com/akitasoftware/akita-libs/tags"
)

const (
	CircleRepositoryURL = "CIRCLE_REPOSITORY_URL"
	CircleBranch = "CIRCLE_BRANCH"
	CircleSHA1 = "CIRCLE_SHA1"
	CirclePullRequest = "CIRCLE_PULL_REQUEST"
	CircleBuildURL = "CIRCLE_BUILD_URL"
)

// https://circleci.com/docs/2.0/env-vars/#built-in-environment-variables
func circleCIInfo() (*github.PullRequest, map[tags.Key]string) {
	pr, err := github.ParsePullRequestURL(os.Getenv(CirclePullRequest))
	if err == nil {
		pr.Branch = os.Getenv(CircleBranch)
		pr.Commit = os.Getenv(CircleSHA1)
	} else {
		debugln("Unable to determine GitHub PR from environment. Is `CIRCLE_PULL_REQUEST` set correctly?")
	}

	tags := map[tags.Key]string{
		tags.XAkitaCI:               CircleCI.String(),
		tags.XAkitaGitRepoURL:       os.Getenv(CircleRepositoryURL),
		tags.XAkitaGitBranch:        os.Getenv(CircleBranch),
		tags.XAkitaGitCommit:        os.Getenv(CircleSHA1),
		tags.XAkitaGitHubPRURL:      os.Getenv(CirclePullRequest),
		tags.XAkitaCircleCIBuildURL: os.Getenv(CircleBuildURL),
	}
	debugDumpEnv([]string{
		CircleRepositoryURL,
		CircleBranch,
		CircleSHA1,
		CirclePullRequest,
		CircleBuildURL,
	})
	return pr, tags
}
