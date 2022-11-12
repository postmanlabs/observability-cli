package env

import (
	"net"
	"os"
)

// Returns true if the CLI is running inside the official docker release image.
func InDocker() bool {
	_, inDocker := os.LookupEnv("__X_AKITA_CLI_DOCKER")
	return inDocker
}

const dockerInternalHostname = "host.docker.internal"

// Docker exposes an internal address for containers to communicate with the
// host.  It is only available on Docker Desktop and indicates the container
// is being run by Docker Desktop on macOS or Windows.
// https://docs.docker.com/desktop/networking/#i-want-to-connect-from-a-container-to-a-service-on-the-host
func HasDockerInternalHostAddress() bool {
	ips, err := net.LookupIP(dockerInternalHostname)
	return err == nil && len(ips) > 0
}
