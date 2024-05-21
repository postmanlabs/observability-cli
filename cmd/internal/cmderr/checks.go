package cmderr

import (
	"errors"

	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/env"
	"github.com/akitasoftware/akita-cli/printer"
)

// Checks that a user has configured their Postman API key and returned them.
// If the user has not configured their API key, a user-friendly error message is printed and an error is returned.
func RequirePostmanAPICredentials(explanation string) (string, error) {
	key, _ := cfg.GetPostmanAPIKeyAndEnvironment()
	if key == "" {
		printer.Errorf("No Postman API key configured. %s\n", explanation)
		if env.InDocker() {
			printer.Infof("Please set the POSTMAN_API_KEY environment variable on the Docker command line.\n")
		} else {
			printer.Infof("Please set the POSTMAN_API_KEY environment variable, either in your shell session or prepend it to postman-insights-agent command.\n")
		}
		//lint:ignore ST1005 This is a user-facing error message
		return "", AkitaErr{Err: errors.New("Could not find a Postman API key to use")}
	}

	return key, nil
}
