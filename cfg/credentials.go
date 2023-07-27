package cfg

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
)

// Credential config can be set in 2 ways:
//
//  1. Via YAML config file under $HOME/.akita/credentials.yaml
//     The file layout is a map mapping profile to the API key ID and secret
//     (currently only default profile is supported). For example:
//
//     ```yaml
//     default:
//     api_key_id: apk_6NiejyYEVpWfziUXJgovV6
//     api_key_secret: 09328501313h39tgh91238tg
//     profile-1:
//     api_key_id: apk_XvGyglmvHQoMcq3WOoLly
//     api_key_secret: 34985g298g2498ty243gh2jl
//     ```
//
//  2. Via environment variables `AKITA_API_KEY_ID` and `AKITA_API_KEY_SECRET`.
var creds = viper.New()

const (
	credsFileName = "credentials"
)

// Create the config file if it doesn't exist.
func writeConfigToFile(profile string, keyValueMap map[string]string) error {
	if profile != "default" {
		return errors.Errorf("non-default profile not supported yet")
	}

	credsFile := GetCredentialsConfigPath()
	if _, err := os.Stat(credsFile); os.IsNotExist(err) {
		// Create initial config file.
		if f, err := os.OpenFile(credsFile, os.O_CREATE|os.O_EXCL, 0600); err != nil {
			return errors.Wrapf(err, "failed to create %s", credsFile)
		} else {
			f.Close()
		}
	} else if err != nil {
		return errors.Wrapf(err, "failed to stat %s", credsFile)
	}

	for key, value := range keyValueMap {
		creds.Set(profile+"."+key, value)
	}

	return creds.WriteConfig()
}

func initCreds() {
	// Set up credentials to read from config file.
	creds.SetConfigType("yaml")
	creds.AddConfigPath(cfgDir)
	creds.SetConfigName(credsFileName)

	// Allow credentials to be set via the environment.
	// API keys from env are implicitly in the "default" profile.
	creds.AutomaticEnv()
	creds.BindEnv("default.api_key_id", "AKITA_API_KEY_ID")
	creds.BindEnv("default.api_key_secret", "AKITA_API_KEY_SECRET")

	if err := creds.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Ignore config file not found error since the config may be set by
			// environment variables or the user could be running the login command
			// to initialize the config.
		} else {
			fmt.Fprintf(os.Stderr, "Failed to read credentials config: %v\n", err)
			os.Exit(2)
		}
	}
}

func GetCredentialsConfigPath() string {
	return filepath.Join(cfgDir, credsFileName+".yaml")
}

func GetAPIKeyAndSecret() (string, string) {
	// Only support default profile for now.
	return creds.GetString("default.api_key_id"), creds.GetString("default.api_key_secret")
}

// Writes API key ID and secret to the config file.
func WriteAPIKeyAndSecret(profile, keyID, keySecret string) error {
	keyValueMap := map[string]string{
		"api_key_id":     keyID,
		"api_key_secret": keySecret,
	}

	return writeConfigToFile(profile, keyValueMap)
}

// Get Postman API key and environment from config file
func GetPostmanAPIKeyAndEnvironment() (string, string) {
	// Only support default profile for now.
	return creds.GetString("default.postman_api_key"), creds.GetString("default.postman_env")
}

// Writes Postman API key and environment to the config file.
func WritePostmanAPIKeyAndEnvironment(profile, postmanAPIKey string, postmanEnvironment string) error {
	keyValueMap := map[string]string{
		"postman_api_key": postmanAPIKey,
		"postman_env":     postmanEnvironment,
	}

	return writeConfigToFile(profile, keyValueMap)
}
