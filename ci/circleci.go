package ci

import (
	"os"

	"github.com/akitasoftware/akita-libs/github"
	"github.com/akitasoftware/akita-libs/tags"
)

// https://circleci.com/docs/2.0/env-vars/#built-in-environment-variables
func circleCIInfo() (*github.PullRequest, map[tags.Key]string) {
	pr, err := github.ParsePullRequestURL(os.Getenv("CIRCLE_PULL_REQUEST"))
	if err == nil {
		pr.Branch = os.Getenv("CIRCLE_BRANCH")
		pr.Commit = os.Getenv("CIRCLE_SHA1")
	}

	tags := map[tags.Key]string{
		tags.XAkitaCI:               CircleCI.String(),
		tags.XAkitaCircleCIBuildURL: os.Getenv("CIRCLE_BUILD_URL"),
	}
	return pr, tags
}
