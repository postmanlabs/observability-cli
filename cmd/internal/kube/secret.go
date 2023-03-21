package kube

import (
	"bytes"
	"encoding/json"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/ghodss/yaml"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8_json "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"os"
	"path/filepath"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	outputFlag    string
	namespaceFlag string
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Generate a Kubernetes secret containing the Akita credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		key, secret, err := cmderr.RequireAPICredentials("Akita API key is required for Kubernetes Secret generation")
		if err != nil {
			return err
		}

		output, err := handleSecretGeneration(namespaceFlag, key, secret, outputFlag)
		if err != nil {
			return err
		}

		// Output the generated secret to the console
		printer.RawOutput(output)

		return nil
	},
	// Override the parent command's PersistentPreRun to prevent any logs from being printed.
	// This is necessary because the secret command is intended to be used in a pipeline
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize the telemetry client, but do not allow any logs to be printed
		telemetry.Init(false)
	},
}

/*
XXX: Kuberenetes Go API package currently has issues with valid serialization.
The ObjectMeta field's CreationTimestamp field is improperly serialized as null when it should be omitted entirely if it is a zero value.
This shouldn't cause any issues applying the secret, but it does cause issues for any tools that depend on valid yaml objects (such as linting tools)
See: https://github.com/kubernetes/kubernetes/issues/109427

Here, I've manually filtered out the CreationTimestamp field from the serialized object to work around this issue.
*/
func buildSecretConfiguration(namespace, apiKey, apiSecret string) ([]byte, error) {
	secret := &v1.Secret{
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
			"akita-api-key":    []byte(apiKey),
			"akita-api-secret": []byte(apiSecret),
		},
	}

	unstructuredSecret, err := runtime.DefaultUnstructuredConverter.ToUnstructured(secret)
	if err != nil {
		return nil, err
	}

	unstructuredObj := &unstructured.Unstructured{Object: unstructuredSecret}
	serializer := k8_json.NewSerializerWithOptions(
		k8_json.DefaultMetaFactory,
		nil,
		nil,
		k8_json.SerializerOptions{Yaml: false, Pretty: false, Strict: true},
	)

	buf := bytes.NewBuffer([]byte{})
	err = serializer.Encode(unstructuredObj, buf)
	if err != nil {
		return nil, err
	}

	// HACK: Manually filter out the CreationTimestamp field from the serialized object
	objMap := make(map[string]interface{})
	err = json.Unmarshal(buf.Bytes(), &objMap)
	if err != nil {
		return nil, err
	}

	if _, ok := objMap["metadata"]; ok {
		metadataMap := objMap["metadata"].(map[string]interface{})
		delete(metadataMap, "creationTimestamp")
	}

	// Re-serialize the object
	fixedJSON, err := json.Marshal(objMap)
	if err != nil {
		return nil, err
	}

	return yaml.JSONToYAML(fixedJSON)
}

// Generates a Kubernetes secret config file for Akita
func handleSecretGeneration(namespace, apiKey, apiSecret, output string) (string, error) {

	secret, err := buildSecretConfiguration(namespace, apiKey, apiSecret)
	if err != nil {
		return "", cmderr.AkitaErr{Err: errors.Wrap(err, "failed to generate Kubernetes secret")}
	}

	// Serialize the secret to YAML
	secretFile, err := createSecretFile(output)
	if err != nil {
		return "", cmderr.AkitaErr{Err: errors.Wrap(err, "failed to create output file")}
	}
	defer secretFile.Close()

	_, err = secretFile.Write(secret)
	if err != nil {
		return "", cmderr.AkitaErr{Err: errors.Wrap(err, "failed to generate template")}
	}

	return string(secret), nil
}

// Creates a file at the give path to be used for storing of the generated Secret config
// If any child dicrectories do not exist, it will be created.
func createSecretFile(path string) (*os.File, error) {
	// Split the outut flag value into directory and filename
	outputDir, outputName := filepath.Split(path)

	// Get the absolute path of the output directory
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve the absolute path of the output directory")
	}

	// Check that the output directory exists
	if _, statErr := os.Stat(absOutputDir); os.IsNotExist(statErr) {
		return nil, errors.Errorf("output directory %s does not exist", absOutputDir)
	}

	// Check if the output file already exists
	outputFilePath := filepath.Join(absOutputDir, outputName)
	if _, statErr := os.Stat(outputFilePath); statErr == nil {
		return nil, errors.Errorf("output file %s already exists", outputFilePath)
	}

	// Create the output file in the output directory
	outputFile, err := os.Create(outputFilePath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the output file")
	}

	return outputFile, nil
}

func init() {
	secretCmd.Flags().StringVarP(
		&namespaceFlag,
		"namespace",
		"n",
		"default",
		"The Kubernetes namespace the secret should be applied to",
	)

	secretCmd.Flags().StringVarP(&outputFlag, "output", "o", "akita-secret.yml", "File to output the generated secret.")

	Cmd.AddCommand(secretCmd)
}
