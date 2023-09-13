package ci

import (
	"fmt"
	"os"
	"strconv"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/github"
	"github.com/akitasoftware/akita-libs/tags"
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

// Implements encoding.TextMarshaler
func (c CI) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

// Implements encoding.TextUnmarshaler
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

// Indicates whether CI debugging information has been printed. Ensures we
// print this information at most once.
var debugged = false

// Only outputs when `debugged` is false
func debugf(fmtString string, args ...interface{}) {
	if !debugged {
		printer.Debugf(fmtString, args...)
	}
}

// Only outputs when `debugged` is false
func debugln(args ...interface{}) {
	if !debugged {
		printer.Debugln(args...)
	}
}

// Prints a debug message indicating the value of an environment variable.
// Only outputs when `debugged` is false.
func debugEnv(varValue string, haveVar bool, fmtString string, args ...interface{}) {
	if !debugged {
		message := fmt.Sprintf(fmtString, args...)
		if haveVar {
			message += fmt.Sprintf(" (currently set to `%s`)", varValue)
		} else {
			message += " (currently unset)"
		}
		debugln(message)
	}
}

// Dumps a set of environment variables. Only outputs when `debugged` is
// false.
func debugDumpEnv(vars []string) {
	if debugged {
		return
	}

	debugln("Partial dump of environment:")
	for _, name := range vars {
		value, haveVar := os.LookupEnv(name)
		if haveVar {
			debugf("  %s=%s\n", name, value)
		} else {
			debugf("  %s is unset\n", name)
		}
	}
}

// Returns learn session tags for common CI environments.
// Currently, we support:
// - CircleCI
// - TravisCI
func GetCIInfo() (CI, *github.PullRequest, map[tags.Key]string) {
	// Only do debug logging the first time this is called.
	defer func() { debugged = true }()

	ciValue, haveCI := os.LookupEnv("CI")
	if inCI, err := strconv.ParseBool(ciValue); err != nil || !inCI {
		debugEnv(ciValue, haveCI, "CI not detected: `CI` is not set to `TRUE` in the environment")
		return Unknown, nil, nil
	}

	if circleci, err := strconv.ParseBool(os.Getenv("CIRCLECI")); err == nil && circleci {
		debugln("Detected Circle CI environment")
		pr, tags := circleCIInfo()
		return CircleCI, pr, tags
	}
	if travis, err := strconv.ParseBool(os.Getenv("TRAVIS")); err == nil && travis {
		debugln("Detected Travis environment")
		pr, tags := travisCIInfo()
		return Travis, pr, tags
	}

	debugln("Detected unknown CI environment")
	debugDumpEnv([]string{
		"CI",
		"CIRCLECI",
		"TRAVIS",
	})
	return Unknown, nil, nil
}
