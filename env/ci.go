package env

import (
	"net/url"
	"os"
	"path"
	"strconv"
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
func GetCITagsForLearnSession() (CI, map[string]string) {
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
func circleCITags() map[string]string {
	return map[string]string{
		"x-akita-ci":                 CircleCI.String(),
		"x-akita-git-repo-url":       os.Getenv("CIRCLE_REPOSITORY_URL"),
		"x-akita-git-branch":         os.Getenv("CIRCLE_BRANCH"),
		"x-akita-git-commit":         os.Getenv("CIRCLE_SHA1"),
		"x-akita-github-pr-url":      os.Getenv("CIRCLE_PULL_REQUEST"),
		"x-akita-circleci-build-url": os.Getenv("CIRCLE_BUILD_URL"),
	}
}

// Travis environment doesn't specify whether the repo or PR is on GitHub or
// some other provider (e.g. BitBucket). For now, we assume everything is on
// GitHub.
// https://docs.travis-ci.com/user/environment-variables/#default-environment-variables
func travisCITags() map[string]string {
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

	return map[string]string{
		"x-akita-ci":                   TravisCI.String(),
		"x-akita-git-repo-url":         repoURL.String(),
		"x-akita-git-branch":           os.Getenv("TRAVIS_BRANCH"),
		"x-akita-git-commit":           os.Getenv("TRAVIS_COMMIT"),
		"x-akita-github-pr-url":        prURL.String(),
		"x-akita-travis-build-web-url": os.Getenv("TRAVIS_BUILD_WEB_URL"),
		"x-akita-travis-job-web-url":   os.Getenv("TRAVIS_JOB_WEB_URL"),
	}
}
