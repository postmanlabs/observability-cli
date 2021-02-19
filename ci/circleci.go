package ci

import (
	"os"

	"github.com/akitasoftware/akita-libs/github"
)

// https://circleci.com/docs/2.0/env-vars/#built-in-environment-variables
func circleCIInfo() (*github.PullRequest, map[string]string) {
	pr, err := github.ParsePullRequestURL(os.Getenv("CIRCLE_PULL_REQUEST"))
	if err == nil {
		pr.Branch = os.Getenv("CIRCLE_BRANCH")
		pr.Commit = os.Getenv("CIRCLE_SHA1")
	}

	tags := map[string]string{
		"x-akita-ci":                 CircleCI.String(),
		"x-akita-circleci-build-url": os.Getenv("CIRCLE_BUILD_URL"),
	}
	return pr, tags
}
