package kube

import (
	"encoding/base64"
	"log"
	"os"
	"text/template"

	"github.com/akitasoftware/akita-cli/printer"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)


var (
	output                string
	namespace             string
	// Store a parsed representation of /template/akita-secret.tmpl
	secretTemplate        *template.Template
)

var secretCmd = &cobra.Command{
	Use: "secret",
	Short: "Generate a Kubernetes secret config for Akita",
	RunE: func(cmd *cobra.Command, args []string) error {
		if namespace == "" {
			return cmderr.AkitaErr{Err: errors.New("namespace flag not set")}
		}

		key, secret, err := cmderr.RequireAPICredentials("Akita API key is required for Kubernetes Secret generation")
		if err != nil {
			return err
		}

		err = handleSecretGeneration(namespace, key, secret, output)	
		if err != nil {
			return err
		}


		printer.Infof("Generated Kubernetes secret config to %s", output)
		return nil
	},
}

// Represents the input used by secretTemplate
type secretTemplateInput struct {
	//
	Namespace string
	APIKey    string
	APISecret string
}

func handleSecretGeneration(namespace, key, secret, output string) error {

	input := secretTemplateInput{
		Namespace: namespace,
		APIKey:    base64.StdEncoding.EncodeToString([]byte(key)),
		APISecret: base64.StdEncoding.EncodeToString([]byte(secret)),
	}

	file, err := os.Create(output)
	if err != nil {
		return cmderr.AkitaErr{Err: errors.Wrap(err, "failed to create output file")}
	}

	defer file.Close()

	err = secretTemplate.Execute(file, input)
	if err != nil {
		return cmderr.AkitaErr{Err: errors.Wrap(err, "failed to generate template")}
	}

	return nil
}

func init() {
	var err error

	secretTemplate, err = template.ParseFS(templateFS, "template/akita-secret.tmpl")
	if err != nil {
		log.Fatalf("unable to parse kube secret template: %v", err)
	}

	// Create a flag on the root subcommand to avoid
	secretCmd.Flags().StringVarP(
		&namespace,
		"namespace",
		"n",
		"",
		"The Kuberenetes namespace the secret should be applied to",
	)

	secretCmd.Flags().StringVarP(&output, "output", "o", "akita-secret.yml", "File to output the generated secret.")

	Cmd.AddCommand(secretCmd)
}
