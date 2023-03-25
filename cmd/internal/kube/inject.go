package kube

import (
	"bytes"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/kube/injector"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/go-utils/optionals"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
)

var (
	// The target Yaml faile to be injected
	// This is required for execution of injectCmd
	injectFileNameFlag string
	// The output file to write the injected Yaml to
	// If not set, injectCmd will default to printing the output to stdout
	injectOutputFlag string
	// The name of the project that the injected deployments should be associated with
	// This will be used by the agent to determine which Akita service to report traffic to
	projectNameFlag string
	// Represents the options for generating a secret
	// When set to "false" or left empty, injectCmd will not generate a secret
	// When set to "true", injectCmd will prepend a secret to each injectable namespace found in the file to inject (injectFileNameFlag)
	// Otherwise, injectCmd will treat secretInjectFlag as the file path all secrets should be generated to
	secretInjectFlag string
)

var injectCmd = &cobra.Command{
	Use:   "inject",
	Short: "Inject Akita into a Kubernetes deployment",
	Long:  "Inject Akita into a Kubernetes deployment or set of deployments, and output the result to stdout or a file",
	RunE: func(_ *cobra.Command, args []string) error {
		secretOpts := resolveSecretGenerationOptions(secretInjectFlag)

		// To avoid users unintentionally attempting to apply injected Deployments via pipeline without
		// their dependent Secrets, require that the user explicitly specify an output file.
		if secretOpts.ShouldInject && secretOpts.Filepath.IsSome() && injectOutputFlag == "" {
			printer.Errorln("Cannot specify a Secret file path without an output file (using --output or -o)")
			printer.Infoln("To generate a Secret file on its own, use `akita kube secret`")
			return cmderr.AkitaErr{
				Err: errors.New("invalid flag usage"),
			}
		}

		// Create the injector which reads from the Kubernetes YAML file specified by the user
		injectr, err := injector.FromYAML(injectFileNameFlag)
		if err != nil {
			return cmderr.AkitaErr{
				Err: errors.Wrapf(
					err,
					"Failed to read injection file %s",
					injectFileNameFlag,
				),
			}
		}

		// Generate a secret for each namespace in the deployment if the user specified secret generation
		secretBuf := new(bytes.Buffer)
		if secretOpts.ShouldInject {
			key, secret, err := cmderr.RequireAPICredentials("API credentials are required to generate secret.")
			if err != nil {
				return err
			}

			namespaces, err := injectr.InjectableNamespaces()
			if err != nil {
				return err
			}

			for _, namespace := range namespaces {
				r, err := handleSecretGeneration(namespace, key, secret)
				if err != nil {
					return err
				}

				secretBuf.WriteString("---\n")
				secretBuf.Write(r)
			}
		}

		// Create the output buffer
		out := new(bytes.Buffer)

		// Either write the secret to a file or prepend it to the output
		if secretFilePath, exists := secretOpts.Filepath.Get(); exists {
			err = writeFile(secretBuf.Bytes(), secretFilePath)
			if err != nil {
				return err
			}

			printer.Infof("Kubernetes Secret generated to %s\n", secretFilePath)
		} else {
			// Assign the secret to the output buffer
			// We do this so that the secret is written before any injected Deployment resources that depend on it
			out = secretBuf
		}

		// Inject the sidecar into the input file
		rawInjected, err := injector.ToRawYAML(injectr, createSidecar(projectNameFlag))
		if err != nil {
			return cmderr.AkitaErr{Err: errors.Wrap(err, "Failed to inject sidecars")}
		}
		// Append the injected YAML to the output
		out.Write(rawInjected)

		// If the user did not specify an output file, print the output to stdout
		if injectOutputFlag == "" {
			printer.Stdout.RawOutput(out.String())
			return nil
		}

		// Write the output to the specified file
		if err := writeFile(out.Bytes(), injectOutputFlag); err != nil {
			return err
		}
		printer.Infof("Injected YAML written to %s\n", injectOutputFlag)

		return nil
	},
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize the telemetry client, but do not allow any logs to be printed
		telemetry.Init(false)
	},
}

// A parsed representation of the `--secret` option.
type secretGenerationOptions struct {
	// Whether to inject a secret
	ShouldInject bool
	// The path to the secret file
	Filepath optionals.Optional[string]
}

func createSidecar(projectName string) v1.Container {
	sidecar := v1.Container{
		Name:  "akita",
		Image: "akitasoftware/cli:latest",
		Env: []v1.EnvVar{
			{
				Name: "AKITA_API_KEY_ID",
				ValueFrom: &v1.EnvVarSource{
					SecretKeyRef: &v1.SecretKeySelector{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "akita-secrets",
						},
						Key: "akita-api-key",
					},
				},
			},
			{
				Name: "AKITA_API_KEY_SECRET",
				ValueFrom: &v1.EnvVarSource{
					SecretKeyRef: &v1.SecretKeySelector{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "akita-secrets",
						},
						Key: "akita-api-secret",
					},
				},
			},
		},
		Lifecycle: &v1.Lifecycle{
			PreStop: &v1.LifecycleHandler{
				Exec: &v1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-c",
						"AKITA_PID=$(pgrep akita) && kill -2 $AKITA_PID && tail -f /proc/$AKITA_PID/fd/1",
					},
				},
			},
		},
		Args: []string{"apidump", "--project", projectName},
		SecurityContext: &v1.SecurityContext{
			Capabilities: &v1.Capabilities{Add: []v1.Capability{"NET_RAW"}},
		},
	}

	return sidecar
}

// Parses the given value for the `--secret` option.
func resolveSecretGenerationOptions(flagValue string) secretGenerationOptions {
	if flagValue == "" || flagValue == "false" {
		return secretGenerationOptions{
			ShouldInject: false,
			Filepath:     optionals.None[string](),
		}
	}

	if flagValue == "true" {
		return secretGenerationOptions{
			ShouldInject: true,
			Filepath:     optionals.None[string](),
		}
	}

	return secretGenerationOptions{
		ShouldInject: true,
		Filepath:     optionals.Some(flagValue),
	}
}

func init() {
	injectCmd.Flags().StringVarP(
		&injectFileNameFlag,
		"file",
		"f",
		"",
		"Path to the Kubernetes YAML file to be injected. This should contain a Deployment object.",
	)
	_ = injectCmd.MarkFlagRequired("file")

	injectCmd.Flags().StringVarP(
		&injectOutputFlag,
		"output",
		"o",
		"",
		"Path to the output file. If not specified, the output will be printed to stdout.",
	)

	injectCmd.Flags().StringVarP(
		&projectNameFlag,
		"project",
		"p",
		"",
		"Name of the Akita project to which the traffic will be uploaded.",
	)
	_ = injectCmd.MarkFlagRequired("project")

	injectCmd.Flags().StringVarP(
		&secretInjectFlag,
		"secret",
		"s",
		"false",
		`Whether to generate a Kubernetes Secret. If set to "true", the secret will be prepended to the modified Kubernetes YAML file. Specify a path to write the secret to a separate file.`,
	)
	// Default value is "true" when the flag is given without an argument.
	injectCmd.Flags().Lookup("secret").NoOptDefVal = "true"

	Cmd.AddCommand(injectCmd)
}
