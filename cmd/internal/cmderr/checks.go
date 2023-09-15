package cmderr

import (
	"errors"

	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/env"
	"github.com/akitasoftware/akita-cli/printer"
)

// Checks that a user has configured their API key and secret and returned them.
// If the user has not configured their API key, a user-friendly error message is printed and an error is returned.
func RequireAkitaAPICredentials(explanation string) (string, string, error) {
	key, secret := cfg.GetAPIKeyAndSecret()
	if key == "" || secret == "" {
		printer.Errorf("No Akita API key configured. %s\n", explanation)
		if env.InDocker() {
			printer.Infof("Please set the AKITA_API_KEY_ID and AKITA_API_KEY_SECRET environment variables on the Docker command line.\n")
		} else {
			printer.Infof("Use the AKITA_API_KEY_ID and AKITA_API_KEY_SECRET environment variables, or run 'akita login'.\n")
		}

		return "", "", AkitaErr{Err: errors.New("could not find an Akita API key to use")}
	}

	return key, secret, nil
}

// Checks that a user has configured their Postman API key and returned them.
// If the user has not configured their API key, a user-friendly error message is printed and an error is returned.
func RequirePostmanAPICredentials(explanation string) (string, error) {
	key, _ := cfg.GetPostmanAPIKeyAndEnvironment()
	if key == "" {
		printer.Errorf("No Postman API key configured. %s\n", explanation)
		if env.InDocker() {
			printer.Infof("Please set the POSTMAN_API_KEY environment variables on the Docker command line.\n")
		} else {
			printer.Infof("Use the POSTMAN_API_KEY environment variables, or run 'postman-lc-agent login'.\n")
		}

		return "", AkitaErr{Err: errors.New("could not find an Postman API key to use")}
	}

	return key, nil
}
