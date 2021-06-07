package env

import (
	"net/url"
	"os"
	"path"
	"strconv"

	"github.com/akitasoftware/akita-libs/tags"
)

type CI int

const (
	UnknownCI CI = iota
	CircleCI
	TravisCI
)

func (c CI) String() string {
	switch c {
	case CircleCI:
		return "CircleCI"
	case TravisCI:
		return "Travis"
	default:
		return "Unknown"
	}
}

// Implments encoding.TextMarshaler
func (c CI) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

// Implments encoding.TextUnmarshaler
func (c *CI) UnmarshalText(s []byte) error {
	switch string(s) {
	case "CircleCI":
		*c = CircleCI
	case "Travis":
		*c = TravisCI
	default:
		*c = UnknownCI
	}
	return nil
}

// Returns learn session tags for common CI environments.
// Currently, we support:
// - CircleCI
// - TravisCI
func GetCITagsForLearnSession() (CI, map[tags.Key]string) {
	if inCI, err := strconv.ParseBool(os.Getenv("CI")); err != nil || !inCI {
		return UnknownCI, nil
	}

	if circleci, err := strconv.ParseBool(os.Getenv("CIRCLECI")); err == nil && circleci {
		return CircleCI, circleCITags()
	}
	if travis, err := strconv.ParseBool(os.Getenv("TRAVIS")); err == nil && travis {
		return TravisCI, travisCITags()
	}

	return UnknownCI, nil
}

// https://circleci.com/docs/2.0/env-vars/#built-in-environment-variables
func circleCITags() map[tags.Key]string {
	return map[tags.Key]string{
		tags.XAkitaCI:               CircleCI.String(),
		tags.XAkitaGitRepoURL:       os.Getenv("CIRCLE_REPOSITORY_URL"),
		tags.XAkitaGitBranch:        os.Getenv("CIRCLE_BRANCH"),
		tags.XAkitaGitCommit:        os.Getenv("CIRCLE_SHA1"),
		tags.XAkitaGitHubPRURL:      os.Getenv("CIRCLE_PULL_REQUEST"),
		tags.XAkitaCircleCIBuildURL: os.Getenv("CIRCLE_BUILD_URL"),
	}
}

// Travis environment doesn't specify whether the repo or PR is on GitHub or
// some other provider (e.g. BitBucket). For now, we assume everything is on
// GitHub.
// https://docs.travis-ci.com/user/environment-variables/#default-environment-variables
func travisCITags() map[tags.Key]string {
	repoURL := url.URL{
		Scheme: "https",
		Host:   "github.com",
		Path:   os.Getenv("TRAVIS_REPO_SLUG"),
	}
	prURL := url.URL{
		Scheme: "https",
		Host:   "github.com",
		Path:   path.Join(os.Getenv("TRAVIS_REPO_SLUG"), "pull", os.Getenv("TRAVIS_PULL_REQUEST")),
	}

	return map[tags.Key]string{
		tags.XAkitaCI:                TravisCI.String(),
		tags.XAkitaGitRepoURL:        repoURL.String(),
		tags.XAkitaGitBranch:         os.Getenv("TRAVIS_BRANCH"),
		tags.XAkitaGitCommit:         os.Getenv("TRAVIS_COMMIT"),
		tags.XAkitaGitHubPRURL:       prURL.String(),
		tags.XAkitaTravisBuildWebURL: os.Getenv("TRAVIS_BUILD_WEB_URL"),
		tags.XAkitaTravisJobWebURL:   os.Getenv("TRAVIS_JOB_WEB_URL"),
	}
}
