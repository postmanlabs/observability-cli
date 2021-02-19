package ci

import (
	"os"
	"strconv"

	"github.com/akitasoftware/akita-libs/github"
)

type CI int

const (
	Unknown CI = iota
	CircleCI
	Travis
)

func (c CI) String() string {
	switch c {
	case CircleCI:
		return "CircleCI"
	case Travis:
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
		*c = Travis
	default:
		*c = Unknown
	}
	return nil
}

func GetCIInfo() (CI, *github.PullRequest, map[string]string) {
	if inCI, err := strconv.ParseBool(os.Getenv("CI")); err != nil || !inCI {
		return Unknown, nil, nil
	}

	if circleci, err := strconv.ParseBool(os.Getenv("CIRCLECI")); err == nil && circleci {
		pr, tags := circleCIInfo()
		return CircleCI, pr, tags
	}
	if travis, err := strconv.ParseBool(os.Getenv("TRAVIS")); err == nil && travis {
		pr, tags := travisCIInfo()
		return Travis, pr, tags
	}

	return Unknown, nil, nil
}
