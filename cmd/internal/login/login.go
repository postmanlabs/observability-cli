package login

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cfg"
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

		fmt.Println("Login successful!")
		fmt.Println("API keys stored in", cfg.GetCredentialsConfigPath())
		return nil
	},
}
