package learn

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
	"github.com/akitasoftware/akita-libs/tags"

	"github.com/akitasoftware/akita-cli/apidump"
	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-cli/location"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/akita-cli/util"

	"github.com/akitasoftware/akita-cli/plugin"
)

var Cmd = &cobra.Command{
	Deprecated:   "please use 'apidump' to capture traffic instead. API models are now created automatically in the Akita app.",
	Use:          "learn",
	Short:        "Run learn mode monitor",
	Long:         "Generate API models from network traffic",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := runLearnMode(); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}

func runLearnMode() error {
	clientID := telemetry.GetClientID()

	plugins, err := pluginloader.Load(pluginsFlag)
	if err != nil {
		return errors.Wrap(err, "failed to load plugins")
	}

	// Determine project name from --out.
	var projectName string
	if uri := outFlag.AkitaURI; uri == nil {
		if serviceFlag == "" {
			if outFlag.LocalPath == nil {
				return errors.Errorf("must specify --project")
			} else {
				return errors.Errorf("must specify --project when --out is not an AkitaURI")
			}
		}
		projectName = serviceFlag
	} else {
		projectName = uri.ServiceName

		if serviceFlag != "" && serviceFlag != projectName {
			return errors.Errorf("--project and --out cannot specify different projects")
		}
	}

	tagsMap, err := util.ParseTagsAndWarn(tagsFlag)
	if err != nil {
		return err
	}

	_, err = runAPIDump(clientID, projectName, tagsMap, plugins)
	if err != nil {
		return errors.Wrap(err, "failed to create trace")
	}

	return nil
}

// Captures packets from the network and adds them to a trace.
//
// The give tagsMap is expected to already contain information about how the
// trace is captured (e.g., whether the capture was user-initiated or is from
// CI, and any applicable information from CI).
func runAPIDump(clientID akid.ClientID, projectName string, tagsMap map[tags.Key]string, plugins []plugin.AkitaPlugin) (*akiuri.URI, error) {
	// Determine packet filter.
	var packetFilter string
	{
		// Translate --port and --bpf-filter flags
		packetFilter = legacyBPFFlag
		if legacyPortFlag != 0 {
			if packetFilter != "" {
				return nil, errors.Errorf("cannot specify both --port and --bpf-filter")
			}
			packetFilter = fmt.Sprintf("port %d", legacyPortFlag)
		}

		// --filter flag trumps legacy --bpf-filter and --port flags.
		if filterFlag != "" {
			packetFilter = filterFlag
		}
	}

	// Always store the trace on Akita Cloud, optionally the user may tee the
	// results to local HAR files using legacy --har flags.
	var traceOut location.Location
	if legacySessionFlag != "" {
		// Attach to an existing learn session.

		// Convert learn session ID to AkitaURI.
		var lrn akid.LearnSessionID
		if err := akid.ParseIDAs(legacySessionFlag, &lrn); err != nil {
			return nil, errors.Wrapf(err, "%q is not a valid learn session id", legacySessionFlag)
		}

		frontClient := rest.NewFrontClient(rest.Domain, clientID)
		svc, err := util.GetServiceIDByName(frontClient, projectName)
		if err != nil {
			return nil, errors.Wrap(err, "failed to look up project id from name")
		}

		learnClient := rest.NewLearnClient(rest.Domain, clientID, svc)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		session, err := learnClient.GetLearnSession(ctx, svc, lrn)
		cancel()
		if err != nil {
			return nil, errors.Wrapf(err, "failed to look up learn session %s", akid.String(lrn))
		} else if session.Name == "" {
			// This should never happen since we migrated all learn sessions to use
			// the ID as the default name.
			return nil, errors.Errorf("%q is an unnamed learn session", legacySessionFlag)
		}

		traceOut.AkitaURI = &akiuri.URI{
			ServiceName: projectName,
			ObjectType:  akiuri.TRACE.Ptr(),
			ObjectName:  session.Name,
		}
	} else {
		traceOut.AkitaURI = &akiuri.URI{
			ServiceName: projectName,
			ObjectType:  akiuri.TRACE.Ptr(),
		}
	}

	// If legacy HAR mode is enabled, tee the trace to local files as well.
	if legacyHARFlag {
		traceOut.LocalPath = &legacyHARDirFlag
		printer.Infof("Akita will also store your API traffic as HAR files in: %s\n", legacyHARDirFlag)
	}

	// --sample-rate overrides legacy --har_sample_rate
	sampleRate := sampleRateFlag
	if legacyHARFlag && sampleRateFlag == 1.0 {
		sampleRate = legacyHARSampleFlag
	}

	// Rate limit must be greater than zero.
	if rateLimitFlag <= 0.0 {
		rateLimitFlag = 1000.0
	}

	// Create a trace on the cloud.
	args := apidump.Args{
		ClientID:                clientID,
		Domain:                  rest.Domain,
		Out:                     traceOut,
		Interfaces:              interfacesFlag,
		Filter:                  packetFilter,
		Tags:                    tagsMap,
		SampleRate:              sampleRate,
		WitnessesPerMinute:      rateLimitFlag,
		PathExclusions:          pathExclusionsFlag,
		HostExclusions:          hostExclusionsFlag,
		PathAllowlist:           pathAllowlistFlag,
		HostAllowlist:           hostAllowlistFlag,
		ExecCommand:             execCommandFlag,
		ExecCommandUser:         execCommandUserFlag,
		Plugins:                 plugins,
		LearnSessionLifetime:    apispec.DefaultTraceRotateInterval,
		Deployment:              apispec.DefaultDeployment,
		StatsLogDelay:           statsLogDelay,
		TelemetryInterval:       apispec.DefaultTelemetryInterval_seconds,
		ProcFSPollingInterval:   apispec.DefaultProcFSPollingInterval_seconds,
		CollectTCPAndTLSReports: apispec.DefaultCollectTCPAndTLSReports,
		ParseTLSHandshakes:      apispec.DefaultParseTLSHandshakes,
		MaxWitnessSize_bytes:    apispec.DefaultMaxWitnessSize_bytes,
	}

	return traceOut.AkitaURI, apidump.Run(args)
}
