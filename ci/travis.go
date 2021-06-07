package ci

import (
	"net/url"
	"os"
	"path"

	"github.com/akitasoftware/akita-libs/github"
	"github.com/akitasoftware/akita-libs/tags"
)

// Travis environment doesn't specify whether the repo or PR is on GitHub or
// some other provider (e.g. BitBucket). For now, we assume everything is on
// GitHub.
// https://docs.travis-ci.com/user/environment-variables/#default-environment-variables
func travisCIInfo() (*github.PullRequest, map[tags.Key]string) {
	prURL := url.URL{
		Scheme: "https",
		Host:   "github.com",
		Path:   path.Join(os.Getenv("TRAVIS_REPO_SLUG"), "pull", os.Getenv("TRAVIS_PULL_REQUEST")),
	}

	pr, err := github.ParsePullRequestURL(prURL.String())
	if err == nil {
		pr.Branch = os.Getenv("TRAVIS_BRANCH")
		pr.Commit = os.Getenv("TRAVIS_COMMIT")
	}

	return pr, map[tags.Key]string{
		tags.XAkitaCI:                Travis.String(),
		tags.XAkitaTravisBuildWebURL: os.Getenv("TRAVIS_BUILD_WEB_URL"),
		tags.XAkitaTravisJobWebURL:   os.Getenv("TRAVIS_JOB_WEB_URL"),
	}
}
