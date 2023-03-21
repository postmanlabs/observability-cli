package kube

import (
	_ "embed"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
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
	result, err := handleSecretGeneration(namespace, key, secret, actualOutput)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	// THEN
	data, err := os.ReadFile(actualOutput)
	if err != nil {
		t.Errorf("Failed to read generated data: %v", err)
	}

	convert := func(yamlBytes []byte) (v1.Secret, error) {
		var result v1.Secret

		jsonData, err := yaml.YAMLToJSONStrict(yamlBytes)
		if err != nil {
			return result, err
		}

		var parsedSecret v1.Secret
		err = json.Unmarshal(jsonData, &parsedSecret)

		return parsedSecret, err
	}

	file, err := convert(data)
	output, err := convert([]byte(result))

	expected := v1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "akita-secrets",
			Namespace: namespace,
		},
		Type: v1.SecretTypeOpaque,
		Data: map[string][]byte{
			"akita-api-key":    []byte(key),
			"akita-api-secret": []byte(secret),
		},
	}

	assert.Equal(t, expected, file)
	assert.Equal(t, expected, output)
}
