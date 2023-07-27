package kube

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/assert"
)

//go:embed test_resource/akita-secret.yml
var testAkitaSecretYAML []byte

//go:embed test_resource/postman-secret.yml
var testPostmanSecretYAML []byte

func TestSecretGeneration(t *testing.T) {
	// GIVEN
	const (
		namespace = "default"
		key       = "api-key"
		secret    = "api-secret"
	)

	// WHEN
	output, err := handleAkitaSecretGeneration(namespace, key, secret)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	// THEN
	assert.Equal(t, testAkitaSecretYAML, output)
}

func TestPostmanSecretGeneration(t *testing.T) {
	// GIVEN
	const (
		namespace = "default"
		key       = "postman-api-key"
	)

	// WHEN
	output, err := handlePostmanSecretGeneration(namespace, key)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	// THEN
	assert.Equal(t, testPostmanSecretYAML, output)
}
