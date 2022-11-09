package architecture

import "runtime"

// Returns the target architecture (from runtime.GOARCH) formatted to be
// consistent with the architecture names we use in CLI releases.
func GetCanonicalArch() string {
	var arch string
	switch runtime.GOARCH {
	case "x86_64", "amd64":
		arch = "amd64"
	case "aarch64", "arm64":
		arch = "arm64"
	default:
		arch = runtime.GOARCH
	}

	return arch
}
