package env

import (
	"os"
)

// Returns true if the CLI is running inside the official docker release image.
func InDocker() bool {
	_, inDocker := os.LookupEnv("__X_AKITA_CLI_DOCKER")
	return inDocker
}
