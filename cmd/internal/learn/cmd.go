package learn

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	randomdata "github.com/Pallinder/go-randomdata"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
	"github.com/akitasoftware/akita-libs/gitlab"
	"github.com/akitasoftware/akita-libs/tags"
	"github.com/akitasoftware/akita-libs/version_names"

	"github.com/akitasoftware/akita-cli/apidump"
	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/cmd/internal/pluginloader"
	"github.com/akitasoftware/akita-cli/location"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"

	"github.com/akitasoftware/akita-cli/plugin"
)

var Cmd = &cobra.Command{
	Use:          "learn",
	Short:        "Run learn mode monitor",
	Long:         "Generate API specifications from network traffic with Akita Learn Mode!",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := runLearnMode(); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}

func runLearnMode() error {
	clientID := akiflag.GetClientID()

	plugins, err := pluginloader.Load(pluginsFlag)
	if err != nil {
		return errors.Wrap(err, "failed to load plugins")
	}

	// XXX Some of this input validation duplicates the input validation for `apispec` (and maybe `apidump`). We should refactor this.

	// Determine service name and validate --out.
	var serviceName string
	if uri := outFlag.AkitaURI; uri == nil {
		if serviceFlag == "" {
			return errors.Errorf("must specify --service when --out is not an AkitaURI")
		}
		serviceName = serviceFlag
	} else {
		serviceName = uri.ServiceName

		if serviceFlag != "" && serviceFlag != serviceName {
			return errors.Errorf("--service and --out cannot specify different services")
		}

		// If an object type is provided, it must be Spec.
		if uri.ObjectType != nil && !uri.ObjectType.IsSpec() {
			return errors.Errorf("output AkitaURI must refer to a spec object")
		}
	}

	// Resolve service name and get a learn client.
	frontClient := rest.NewFrontClient(akiflag.Domain, clientID)
	svc, err := util.GetServiceIDByName(frontClient, serviceName)
	if err != nil {
		return errors.Wrapf(err, "failed to lookup service %q", serviceName)
	}
	learnClient := rest.NewLearnClient(akiflag.Domain, clientID, svc)

	// If a spec name was given, check if the spec already exists.
	if uri := outFlag.AkitaURI; uri != nil && uri.ObjectName != "" {
		if _, err := util.ResolveSpecURI(learnClient, *uri); err == nil {
			return errors.Errorf("spec %q already exists", uri)
		} else {
			var httpErr rest.HTTPError
			if ok := errors.As(err, &httpErr); ok && httpErr.StatusCode != 404 {
				return errors.Wrap(err, "failed to check whether output spec exists already")
			}
		}
	}

	// Support legacy --extend.
	// Resolve the API spec ID or version label to learn session AkitaURIs.
	var legacyExtendTraces []*akiuri.URI
	if legacyExtendFlag != "" {
		var specID akid.APISpecID
		if err := akid.ParseIDAs(legacyExtendFlag, &specID); err != nil {
			// The flag is a version label, resolve that.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			v, err := learnClient.GetSpecVersion(ctx, legacyExtendFlag)
			if err != nil {
				return errors.Wrapf(err, "failed to resolve API spec version label %q", legacyExtendFlag)
			}
			specID = v.APISpecID
		}

		var lrns []akid.LearnSessionID
		{
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			spec, err := learnClient.GetSpec(ctx, specID, rest.GetSpecOptions{})
			if err != nil {
				return errors.Wrapf(err, "failed to lookup extend spec %q", akid.String(specID))
			}

			if len(spec.LearnSessionIDs) > 0 {
				lrns = spec.LearnSessionIDs
			} else if spec.LearnSessionID != nil {
				lrns = append(lrns, *spec.LearnSessionID)
			} else {
				return errors.Errorf("extend spec has no learn session: %q", akid.String(specID))
			}
		}

		// Resolve learn session ID to name.
		for _, lrn := range lrns {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := learnClient.GetLearnSession(ctx, svc, lrn)
			if err != nil {
				return errors.Wrapf(err, "failed to lookup extend session %q", akid.String(lrn))
			}

			legacyExtendTraces = append(legacyExtendTraces, &akiuri.URI{
				ServiceName: serviceName,
				ObjectType:  akiuri.TRACE.Ptr(),
				ObjectName:  session.Name,
			})
		}
	}

	tagsMap, err := tags.FromPairs(tagsFlag)
	if err != nil {
		return err
	}

	// Check for reserved versions.
	for _, version := range versionsFlag {
		if version_names.IsReservedVersionName(version) {
			return errors.Errorf("'%s' is an Akita-reserved version", version)
		}
	}

	// Populate legacy github integration tag.
	// See https://github.com/akitasoftware/superstar/blob/93c546b6522453c277507696d1fefd56d52d6c55/services/witness_processor/github/util.go#L36
	if legacyGitHubURLFlag != "" {
		githubURL, err := url.Parse(legacyGitHubURLFlag)
		if err != nil {
			return errors.Wrap(err, "failed to parse github URL flag")
		}
		tagsMap[tags.XAkitaGitHubPRURL] = path.Join(githubURL.Path, "pull", strconv.Itoa(githubPRFlag))

		// Tag as coming from CI.
		tagsMap[tags.XAkitaSource] = tags.CISource
	}
	// Other tags are set up in apidump and/or apispec
	// TODO: completely refactor so tags are only set here, or
	// internal/apidump/cmd, or internal/apispec/cmd, instead
	// of doing it twice in this case.

	traceURI, err := runAPIDump(clientID, serviceName, tagsMap, plugins)
	if err != nil {
		return errors.Wrap(err, "failed to create trace")
	}

	if err := runAPISpec(clientID, serviceName, traceURI, tagsMap, legacyExtendTraces, plugins); err != nil {
		return errors.Wrap(err, "failed to create spec")
	}

	return nil
}

// Captures packets from the network and adds them to a trace.
//
// The give tagsMap is expected to already contain information about how the
// trace is captured (e.g., whether the capture was user-initiated or is from
// CI, and any applicable information from CI).
func runAPIDump(clientID akid.ClientID, serviceName string, tagsMap map[tags.Key]string, plugins []plugin.AkitaPlugin) (*akiuri.URI, error) {
	// Determing packet filter.
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

		frontClient := rest.NewFrontClient(akiflag.Domain, clientID)
		svc, err := util.GetServiceIDByName(frontClient, serviceName)
		if err != nil {
			return nil, errors.Wrap(err, "failed to lookup service id from name")
		}

		learnClient := rest.NewLearnClient(akiflag.Domain, clientID, svc)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		session, err := learnClient.GetLearnSession(ctx, svc, lrn)
		cancel()
		if err != nil {
			return nil, errors.Wrapf(err, "failed to lookup learn session %s", akid.String(lrn))
		} else if session.Name == "" {
			// This should never happen since we migrated all learn sessions to use
			// the ID as the default name.
			return nil, errors.Errorf("%q is an unnamed learn session", legacySessionFlag)
		}

		traceOut.AkitaURI = &akiuri.URI{
			ServiceName: serviceName,
			ObjectType:  akiuri.TRACE.Ptr(),
			ObjectName:  session.Name,
		}
	} else {
		// Create a new trace.
		var traceName string
		uid := uuid.New()
		traceName = strings.Join([]string{
			randomdata.Adjective(),
			randomdata.Noun(),
			uid.String()[0:8],
		}, "-")

		traceOut.AkitaURI = &akiuri.URI{
			ServiceName: serviceName,
			ObjectType:  akiuri.TRACE.Ptr(),
			ObjectName:  traceName,
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

	// Create a trace on the cloud.
	args := apidump.Args{
		ClientID:           clientID,
		Domain:             akiflag.Domain,
		Out:                traceOut,
		Interfaces:         interfacesFlag,
		Filter:             packetFilter,
		Tags:               tagsMap,
		SampleRate:         sampleRate,
		WitnessesPerMinute: rateLimitFlag,
		PathExclusions:     pathExclusionsFlag,
		HostExclusions:     hostExclusionsFlag,
		PathAllowlist:      pathAllowlistFlag,
		HostAllowlist:      hostAllowlistFlag,
		ExecCommand:        execCommandFlag,
		ExecCommandUser:    execCommandUserFlag,
		Plugins:            plugins,
	}

	return traceOut.AkitaURI, apidump.Run(args)
}

func runAPISpec(clientID akid.ClientID, serviceName string, traceURI *akiuri.URI, tagsMap map[tags.Key]string, legacyExtendTraces []*akiuri.URI, plugins []plugin.AkitaPlugin) error {
	githubRepo, err := getGitHubRepo()
	if err != nil {
		return err
	}

	traces := []location.Location{location.Location{AkitaURI: traceURI}}
	for _, et := range legacyExtendTraces {
		traces = append(traces, location.Location{AkitaURI: et})
	}

	var gitlabMR *gitlab.MRInfo
	if gitlabProjectFlag != "" {
		gitlabMR = &gitlab.MRInfo{
			Project: gitlabProjectFlag,
			IID:     gitlabMRFlag,
			Branch:  gitlabBranchFlag,
			Commit:  gitlabCommitFlag,
		}
	}

	args := apispec.Args{
		ClientID:       clientID,
		Domain:         akiflag.Domain,
		Traces:         traces,
		Out:            outFlag,
		Service:        serviceName,
		Format:         "yaml",
		Tags:           tagsMap,
		Versions:       versionsFlag,
		PathParams:     pathParamsFlag,
		PathExclusions: pathExclusionsFlag,

		Plugins: plugins,

		GitHubRepo:   githubRepo,
		GitHubBranch: githubBranchFlag,
		GitHubCommit: githubCommitFlag,
		GitHubPR:     githubPRFlag,

		GitLabMR: gitlabMR,
	}

	return apispec.Run(args)
}

// Reconcile legacy --github_repo_url flag with new --github-repo flag.
func getGitHubRepo() (string, error) {
	if githubRepoFlag != "" {
		return githubRepoFlag, nil
	} else if legacyGitHubURLFlag != "" {
		u, err := url.Parse(legacyGitHubURLFlag)
		if err != nil {
			return "", errors.Wrap(err, "failed to parse GitHub repo URL")
		}

		// GitHub URL should look like https://github.com/akitasoftware/superstar
		parts := strings.Split(u.Path, "/")
		if len(parts) < 2 {
			return "", errors.Errorf("failed to get repo owner and name from GitHub repo URL")
		}
		return strings.Join(parts[0:2], "/"), nil
	}
	return "", nil
}
