package kube

import (
	"bytes"

	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/kube/injector"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/akita-cli/util"
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
	// Represents the options for generating a secret
	// When set to "false" or left empty, injectCmd will not generate a secret
	// When set to "true", injectCmd will prepend a secret to each injectable namespace found in the file to inject (injectFileNameFlag)
	// Otherwise, injectCmd will treat secretInjectFlag as the file path all secrets should be generated to
	secretInjectFlag string

	// Postman related flags
	postmanCollectionID string
)

var injectCmd = &cobra.Command{
	Use:   "inject",
	Short: "Inject the Postman Insights Agent into a Kubernetes deployment",
	Long:  "Inject the Postman Insights Agent into a Kubernetes deployment or set of deployments, and output the result to stdout or a file",
	RunE: func(_ *cobra.Command, args []string) error {
		if postmanCollectionID == "" {
			return cmderr.AkitaErr{
				Err: errors.New("--collection must be specified."),
			}
		}

		// Lookup service *first* (if we are remote) ensuring that collectionId
		// or projectName is correct and exists.
		err := lookupService(postmanCollectionID)
		if err != nil {
			return err
		}

		secretOpts := resolveSecretGenerationOptions(secretInjectFlag)

		// To avoid users unintentionally attempting to apply injected Deployments via pipeline without
		// their dependent Secrets, require that the user explicitly specify an output file.
		if secretOpts.ShouldInject && secretOpts.Filepath.IsSome() && injectOutputFlag == "" {
			printer.Errorln("Cannot specify a Secret file path without an output file (using --output or -o)")
			printer.Infoln("To generate a Secret file on its own, use `postman-insights-agent kube secret`")
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
			namespaces, err := injectr.InjectableNamespaces()
			if err != nil {
				return err
			}

			key, err := cmderr.RequirePostmanAPICredentials("Postman API credentials are required to generate secret.")
			if err != nil {
				return err
			}

			for _, namespace := range namespaces {
				r, err := handlePostmanSecretGeneration(namespace, key)
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

		var container v1.Container

		// Inject the sidecar into the input file
		_, env := cfg.GetPostmanAPIKeyAndEnvironment()
		container = createPostmanSidecar(postmanCollectionID, env)

		rawInjected, err := injector.ToRawYAML(injectr, container)
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
		// This function overrides the root command preRun so we need to duplicate the domain setup.
		if rest.Domain == "" {
			rest.Domain = rest.DefaultDomain()
		}

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

// The image to use for the Postman Insights Agent sidecar
const akitaImage = "docker.postman.com/postman-insights-agent:latest"

func createPostmanSidecar(postmanCollectionID string, postmanEnvironment string) v1.Container {
	args := []string{"apidump", "--collection", postmanCollectionID}

	// If a nondefault --domain flag was used, specify it for the container as well.
	if rest.Domain != rest.DefaultDomain() {
		args = append(args, "--domain", rest.Domain)
	}

	envs := []v1.EnvVar{
		{
			Name: "POSTMAN_API_KEY",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: "postman-agent-secrets",
					},
					Key: "postman-api-key",
				},
			},
		},
	}

	if postmanEnvironment != "" {
		envs = append(envs, v1.EnvVar{
			Name:  "POSTMAN_ENV",
			Value: postmanEnvironment,
		})
	}

	sidecar := v1.Container{
		Name:  "postman-insights-agent",
		Image: akitaImage,
		Env:   envs,
		Lifecycle: &v1.Lifecycle{
			PreStop: &v1.LifecycleHandler{
				Exec: &v1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-c",
						"POSTMAN_INSIGHTS_AGENT_PID=$(pgrep postman-insights-agent) && kill -2 $POSTMAN_INSIGHTS_AGENT_PID && tail -f /proc/$POSTMAN_INSIGHTS_AGENT_PID/fd/1",
					},
				},
			},
		},
		Args: args,
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

// Check if collection exists or not
func lookupService(postmanCollectionID string) error {
	frontClient := rest.NewFrontClient(rest.Domain, telemetry.GetClientID())

	_, err := util.GetOrCreateServiceIDByPostmanCollectionID(frontClient, postmanCollectionID)
	if err != nil {
		return err
	}
	return nil
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
		&secretInjectFlag,
		"secret",
		"s",
		"false",
		`Whether to generate a Kubernetes Secret. If set to "true", the secret will be added to the modified Kubernetes YAML file. Specify a path to write the secret to a separate file; if this is done, an output file must also be specified with --output.`,
	)
	// Default value is "true" when the flag is given without an argument.
	injectCmd.Flags().Lookup("secret").NoOptDefVal = "true"

	injectCmd.Flags().StringVar(
		&postmanCollectionID,
		"collection",
		"",
		"Your Postman collection ID.")

	Cmd.AddCommand(injectCmd)
}
