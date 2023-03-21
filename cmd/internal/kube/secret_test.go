package kube

import (
	_ "embed"
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
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

	dir := t.TempDir()
	actualOutput := filepath.Join(dir, "akita-secret.yml")

	// WHEN
	output, err := handleSecretGeneration(namespace, key, secret, actualOutput)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	generatedFile, err := os.ReadFile(actualOutput)
	if err != nil {
		t.Errorf("Failed to read generated generatedFile: %v", err)
	}

	// THEN
	assert.Equal(t, string(testAkitaSecretYAML), string(generatedFile))
	assert.Equal(t, string(testAkitaSecretYAML), output)
}
