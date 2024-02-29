package useragent

import (
	"fmt"
	"regexp"
	"strconv"

	ver "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
)

var (
	uaRegexp = regexp.MustCompile(`^postman-insights-agent\/(.*) \((.*); (.*); (.*)\)$`)
)

// Type of environment where the CLI is running.
type EnvType int

const (
	// CLI running as native binary on a host.
	ENV_HOST EnvType = iota

	// CLI running from our docker image.
	ENV_DOCKER
)

func (e EnvType) String() string {
	switch e {
	case ENV_HOST:
		return "host"
	case ENV_DOCKER:
		return "docker"
	default:
		panic("unknown env type: " + strconv.Itoa(int(e)))
	}
}

func envTypeFromString(s string) (EnvType, error) {
	switch s {
	case "host":
		return ENV_HOST, nil
	case "docker":
		return ENV_DOCKER, nil
	default:
		return ENV_HOST, errors.Errorf(`invalid env type "%s"`, s)
	}
}

type UA struct {
	// Semantic version of the CLI.
	Version *ver.Version

	// OS and architecture that the CLI is running on.
	OS   string
	Arch string

	EnvType EnvType
}

func (ua UA) String() string {
	return fmt.Sprintf("postman-insights-agent/%s (%s; %s; %s)", ua.Version, ua.OS, ua.Arch, ua.EnvType)
}

func FromString(s string) (UA, error) {
	matches := uaRegexp.FindStringSubmatch(s)
	if len(matches) != 5 {
		return UA{}, errors.Errorf("expected 5 matched groups, got %d", len(matches))
	}

	v, err := ver.NewSemver(matches[1])
	if err != nil {
		return UA{}, errors.Wrapf(err, `failed to parse "%s" as a semantic version`, matches[1])
	}

	e, err := envTypeFromString(matches[4])
	if err != nil {
		return UA{}, err
	}

	return UA{
		Version: v,
		OS:      matches[2],
		Arch:    matches[3],
		EnvType: e,
	}, nil
}
