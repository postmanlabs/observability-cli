package version

import (
	"fmt"
	"runtime"
	"strings"

	ver "github.com/hashicorp/go-version"
)

var (
	// Set to the content of CURRENT_VERSION file at link-time with -X flag.
	rawReleaseVersion = "0.0.0"

	releaseVersion = ver.Must(ver.NewSemver(strings.TrimSuffix(rawReleaseVersion, "\n")))

	// Set at link-time with -X flag.
	gitVersion = "unknown"
)

func ReleaseVersion() *ver.Version {
	return releaseVersion
}

// The git SHA that this copy of the CLI is built from.
func GitVersion() string {
	return gitVersion
}

func CLIDisplayString() string {
	return fmt.Sprintf("%s (%s, %s)", releaseVersion.String(), gitVersion, runtime.GOARCH)
}
