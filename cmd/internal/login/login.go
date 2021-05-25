package login

import (
	"context"
	"fmt"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/rest"
)

var Cmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with SuperFuzz",
	Long: `The CLI will prompt you to enter information for your Akita API key.

API key information will be stored in ` + cfg.GetCredentialsConfigPath(),
	SuggestFor:   []string{"signin", "sign-in", "log-in"},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ans := struct {
			APIKeyID     string
			APIKeySecret string
		}{}

		if !checkCreds {
			// Interactive prompt.
			qs := []*survey.Question{
				{
					Name:   "APIKeyID",
					Prompt: &survey.Input{Message: "API Key ID:"},
				},
				{
					Name:   "APIKeySecret",
					Prompt: &survey.Password{Message: "API Key Secret:"},
				},
			}

			if err := survey.Ask(qs, &ans); err != nil {
				return errors.Wrap(err, "failed to get API key ID and secret")
			}

			if err := cfg.WriteAPIKeyAndSecret("default", ans.APIKeyID, ans.APIKeySecret); err != nil {
				return errors.Wrap(err, "failed to save credentials config file")
			}

			fmt.Println("API keys stored in", cfg.GetCredentialsConfigPath())
		}

		// Print credentials, if --show-creds is true.
		if showCreds {
			apiKeyID, apiKeySecret := cfg.GetAPIKeyAndSecret()
			fmt.Printf("Using the following credentials:\n")
			fmt.Printf("  Akita API key ID: %s\n", apiKeyID)
			fmt.Printf("  Akita API key secret: %s\n", apiKeySecret)
		}

		// Test credentials.  If authentication fails, prints an error message.
		return testCredentials()
	},
}

func testCredentials() error {
	// First, make sure the API key ID and secret aren't empty.
	{
		apiKeyID, apiKeySecret := cfg.GetAPIKeyAndSecret()
		idIsEmpty := apiKeyID == ""
		secretIsEmpty := apiKeySecret == ""

		if idIsEmpty && secretIsEmpty {
			return errors.New("Login failed: Akita API key ID and secret are empty.")
		} else if idIsEmpty {
			return errors.New("Login failed: Akita API key ID is empty.")
		} else if secretIsEmpty {
			return errors.New("Login failed: Akita API key secret is empty.")
		}
	}

	// Next, test the creds by trying to get the user's services.
	{
		clientID := akid.GenerateClientID()
		frontClient := rest.NewFrontClient(akiflag.Domain, clientID)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := frontClient.GetServices(ctx)
		if err != nil {
			// If the status code is 401, it means authentication failed on the backend.
			if httpErr, ok := err.(rest.HTTPError); ok && httpErr.StatusCode == 401 {
				showCredsMsg := ""
				if !showCreds {
					showCredsMsg = "  Use --show-creds to see the credentials being used."
				}

				return errors.New(fmt.Sprintf("Login failed: cannot authenticate using provided credentials.%s\n", showCredsMsg))
			}

			// Otherwise, there was some other problem -- print the error and return.
			return errors.New(fmt.Sprintf("Login failed: %s\n", err))
		}
	}

	fmt.Println("Login successful!")
	return nil
}