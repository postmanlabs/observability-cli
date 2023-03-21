package kube

import (
	_ "embed"
	"github.com/stretchr/testify/assert"
	"testing"
)

//go:embed test_resource/akita-secret.yml
var testAkitaSecretYAML []byte

func Test_secretGeneration(t *testing.T) {
	// GIVEN
	const (
		namespace = "default"
		key       = "api-key"
		secret    = "api-secret"
	)

	// WHEN
	output, err := handleSecretGeneration(namespace, key, secret)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	// THEN
	assert.Equal(t, testAkitaSecretYAML, output)
}
