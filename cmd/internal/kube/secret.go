package kube

import (
	"bytes"
	"encoding/base64"
	"text/template"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/pkg/errors"
)

var (
	// Stores a parsed representation of /template/postman-secret.tmpl.
	secretTemplate *template.Template
)

// Represents the input used by secretTemplate
type secretTemplateInput struct {
	Namespace string
	APIKey    string
}

func initSecretTemplate() error {
	var err error
	secretTemplate, err = template.ParseFS(templateFS, "template/postman-secret.tmpl")

	if err != nil {
		return cmderr.AkitaErr{Err: errors.Wrap(err, "failed to parse secret template")}
	}

	return nil
}

// Generates a Kubernetes secret config file for Postman
// On success, the generated output is returned as a string.
func handlePostmanSecretGeneration(namespace, key string) ([]byte, error) {
	err := initSecretTemplate()
	if err != nil {
		return nil, cmderr.AkitaErr{Err: errors.Wrap(err, "failed to initialize secret template")}
	}

	input := secretTemplateInput{
		Namespace: namespace,
		APIKey:    base64.StdEncoding.EncodeToString([]byte(key)),
	}

	buf := bytes.NewBuffer([]byte{})

	err = secretTemplate.Execute(buf, input)
	if err != nil {
		return nil, cmderr.AkitaErr{Err: errors.Wrap(err, "failed to generate template")}
	}

	return buf.Bytes(), nil
}
