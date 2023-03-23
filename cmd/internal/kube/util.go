package kube

import (
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/pkg/errors"
	"os"
	"path/filepath"
)

// Writes the generated secret to the given file path
func writeFile(data []byte, filePath string) error {
	f, err := createFile(filePath)
	if err != nil {
		return cmderr.AkitaErr{
			Err: cmderr.AkitaErr{
				Err: errors.Wrapf(
					err,
					"failed to create file %s",
					filePath,
				),
			},
		}
	}
	defer f.Close()

	_, err = f.Write(data)
	if err != nil {
		return errors.Errorf("failed to write to file %s", filePath)
	}

	return nil
}

// Creates a file at the given path to be used for storing of a Kubernetes configuration object
// If the directory provided does not exist, an error will be returned and the file will not be created
func createFile(path string) (*os.File, error) {
	// Split the output flag value into directory and filename
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
