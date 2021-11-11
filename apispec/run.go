package apispec

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/jpillora/backoff"
	"github.com/pkg/errors"
	"github.com/spf13/viper"

	"github.com/akitasoftware/akita-cli/ci"
	"github.com/akitasoftware/akita-cli/deployment"
	"github.com/akitasoftware/akita-cli/location"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/github"
	"github.com/akitasoftware/akita-libs/gitlab"
	pp "github.com/akitasoftware/akita-libs/path_pattern"
	"github.com/akitasoftware/akita-libs/tags"
	"github.com/akitasoftware/akita-libs/time_span"

	"github.com/akitasoftware/akita-cli/plugin"
)

type Args struct {
	// Required
	ClientID akid.ClientID
	Domain   string
	Traces   []location.Location

	// Optional

	// If unset, defaults to a randomly generated backend learn session.
	Out location.Location

	Service                    string
	Format                     string
	Tags                       map[tags.Key]string
	Versions                   []string
	GetSpecEnableRelatedFields bool
	IncludeTrackers            bool
	PathParams                 []string
	PathExclusions             []string
	Timeout                    *time.Duration
	TimeRange                  *time_span.TimeSpan

	GitHubBranch string
	GitHubCommit string
	GitHubRepo   string
	GitHubPR     int

	GitLabMR *gitlab.MRInfo

	Plugins []plugin.AkitaPlugin

	// Legacy -- used by `akita learn-sessions checkpoint`.
	LearnSessionID *akid.LearnSessionID
}

// Collect the tag set to apply to the specification.
// May modify the arguments based on CI information, and return a github PR
func collectSpecTags(args *Args) (map[tags.Key]string, *github.PRInfo, error) {
	specTags := args.Tags
	if specTags == nil {
		specTags = map[tags.Key]string{}
	}

	// Auto detect CI environment.
	ciType, pr, ciTags := ci.GetCIInfo()
	if ciType != ci.Unknown {
		for k, v := range ciTags {
			specTags[k] = v
		}

		specTags[tags.XAkitaSource] = tags.CISource

		if pr != nil {
			if args.GitHubBranch == "" {
				args.GitHubBranch = pr.Branch
			}
			if args.GitHubCommit == "" {
				args.GitHubCommit = pr.Commit
			}
			if args.GitHubRepo == "" {
				args.GitHubRepo = pr.Repo.Owner + "/" + pr.Repo.Name
			}
			if args.GitHubPR == 0 {
				args.GitHubPR = pr.Num
			}
		}
	}

	// Import information about production or staging environment
	// including, possibly, XAkitaSource
	deployment.UpdateTags(specTags)

	// Set source to user by default.
	if _, ok := specTags[tags.XAkitaSource]; !ok {
		specTags[tags.XAkitaSource] = tags.UserSource
	}

	// additional github or gitlab tags
	var githubPR *github.PRInfo
	if args.GitHubRepo != "" && args.GitHubPR != 0 {
		parts := strings.Split(args.GitHubRepo, "/")
		if len(parts) != 2 {
			return nil, nil, errors.Errorf("github repo name should contain {OWNER}/{NAME}")
		}

		// Add tags to store commit information.
		specTags[tags.XAkitaSource] = tags.CISource
		specTags[tags.XAkitaGitHubRepo] = args.GitHubRepo
		specTags[tags.XAkitaGitHubPR] = strconv.Itoa(args.GitHubPR)
		specTags[tags.XAkitaGitBranch] = args.GitHubBranch
		specTags[tags.XAkitaGitCommit] = args.GitHubCommit

		githubPR = &github.PRInfo{
			RepoOwner: parts[0],
			RepoName:  parts[1],
			Num:       args.GitHubPR,
		}
	}
	if args.GitLabMR != nil {
		// Add tags to store commit information.
		specTags[tags.XAkitaSource] = tags.CISource
		specTags[tags.XAkitaGitLabProject] = args.GitLabMR.Project
		specTags[tags.XAkitaGitLabMRIID] = args.GitLabMR.IID
		specTags[tags.XAkitaGitBranch] = args.GitLabMR.Branch
		specTags[tags.XAkitaGitCommit] = args.GitLabMR.Commit
	}

	return specTags, githubPR, nil
}

func Run(args Args) error {
	var serviceName string
	if uri := args.Out.AkitaURI; uri != nil {
		serviceName = uri.ServiceName
		if args.Service != "" && serviceName != args.Service {
			return errors.Errorf("--service and --out cannot specify different services")
		}
	} else if args.Service == "" {
		return errors.Errorf("must specify --service if --out is not an AkitaURI")
	} else {
		serviceName = args.Service
	}

	frontClient := rest.NewFrontClient(args.Domain, args.ClientID)

	// Resolve service name.
	serviceID, err := util.GetServiceIDByName(frontClient, serviceName)
	if err != nil {
		return err
	}
	learnClient := rest.NewLearnClient(args.Domain, args.ClientID, serviceID)

	// Normalization & validation.
	if uri := args.Out.AkitaURI; uri != nil {
		if uri.ObjectType == nil {
			uri.ObjectType = akiuri.SPEC.Ptr()
		} else if !args.Out.AkitaURI.ObjectType.IsSpec() {
			return errors.Errorf("output AkitaURI must refer to a spec object")
		}

		if args.Out.AkitaURI.ObjectName != "" {
			// Check if the spec already exists.
			if _, err := util.ResolveSpecURI(learnClient, *args.Out.AkitaURI); err == nil {
				return errors.Errorf("spec %q already exists", *args.Out.AkitaURI)
			} else {
				var httpErr rest.HTTPError
				if ok := errors.As(err, &httpErr); ok && httpErr.StatusCode != 404 {
					return errors.Wrap(err, "failed to check whether output spec exists already")
				}
			}
		}
	}

	pathExclusions := make([]*regexp.Regexp, len(args.PathExclusions))
	for i, s := range args.PathExclusions {
		if r, err := regexp.Compile(s); err == nil {
			pathExclusions[i] = r
		} else {
			return errors.Wrapf(err, "bad regular expression for path exclusion: %q", s)
		}
	}

	// Build tag set, extract CI or source-control information
	specTags, githubPR, err := collectSpecTags(&args)
	if err != nil {
		return err
	}

	// Process input.
	learnSessions := []akid.LearnSessionID{}
	localPaths := []string{}
	for _, loc := range args.Traces {
		if loc.AkitaURI != nil {
			// Resolve URI into learn session IDs.
			if lrn, err := resolveTraceURI(args.Domain, args.ClientID, *loc.AkitaURI); err != nil {
				return errors.Wrapf(err, "failed to resolve %s", *loc.AkitaURI)
			} else {
				learnSessions = append(learnSessions, lrn)
			}
		} else if loc.LocalPath != nil {
			localPaths = append(localPaths, *loc.LocalPath)
		}
	}

	// Handle legacy input from `akita learn-sessions checkpoint`.
	if args.LearnSessionID != nil {
		learnSessions = append(learnSessions, *args.LearnSessionID)
	}

	// Turn local traces into learn sessions.
	if len(localPaths) > 0 {
		if lrns, err := uploadLocalTraces(args.Domain, args.ClientID, serviceID, localPaths, args.IncludeTrackers, args.Plugins); err != nil {
			return errors.Wrap(err, "failed to upload local traces")
		} else {
			learnSessions = append(learnSessions, lrns...)
		}
	}

	// Create spec with a random name unless user specified a name.
	printer.Infof("Generating API specification...\n")
	outSpecName := util.RandomAPIModelName()
	if args.Out.AkitaURI != nil && args.Out.AkitaURI.ObjectName != "" {
		outSpecName = args.Out.AkitaURI.ObjectName
	}
	timeout := 10 * time.Second
	if args.Timeout != nil {
		timeout = *args.Timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	outSpecID, err := learnClient.CreateSpec(ctx, outSpecName, learnSessions, rest.CreateSpecOptions{
		Tags:           specTags,
		Versions:       args.Versions,
		PathPatterns:   pathPatternsFromStrings(args.PathParams),
		PathExclusions: pathExclusions,
		GitHubPR:       githubPR,
		GitLabMR:       args.GitLabMR,
		TimeRange:      args.TimeRange,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create new spec")
	}

	// Print spec ID to stdout to make it easy for scripting.
	// We precede it with a message on stderr so when the user is using the CLI
	// interactively, it doesn't look like there's a random spec ID floating
	// around.
	outURI := akiuri.URI{
		ServiceName: serviceName,
		ObjectName:  outSpecName,
		ObjectType:  akiuri.SPEC.Ptr(),
	}
	printer.Stderr.Infof("Your API spec URI is: ")
	fmt.Println(outURI.String())

	specURL := GetSpecURL(args.Domain, serviceID, outSpecID)

	// If the output is a spec on the backend, we can just show a URL to it while
	// it's being asynchronously generated.
	// If the output is a local file, we need to wait until the spec is done and
	// then write it to a file.
	if args.Out.LocalPath == nil {
		// No LocalPath means either AkitaURI is set or Out is not set - both mean
		// backend output only.
		successMsg := printer.Color.Green(fmt.Sprintf("🔎 View your spec at: %s", specURL.String()))
		printer.Infof("%s 🎉\n\n%s\n\n", printer.Color.Green("Success!"), successMsg)
	} else {
		printer.Infof("Waiting for your spec to generate...\n")
		printer.Infof("%s\n", printer.Color.Green(fmt.Sprintf("🔎 Preview your spec at: %s", specURL.String())))

		specContent, err := pollSpecUntilReady(learnClient, outSpecID, args.GetSpecEnableRelatedFields)
		if err != nil {
			return errors.Wrap(err, "failed to download generated spec")
		}

		// Convert spec to JSON from YAML.
		if args.Format == "json" {
			j, err := yaml.YAMLToJSON([]byte(specContent))
			if err != nil {
				return errors.Wrap(err, "failed to convert spec from YAML to JSON")
			}
			specContent = string(j)
		}

		var out io.Writer
		if *args.Out.LocalPath == "-" {
			out = os.Stdout
		} else {
			f, err := os.OpenFile(*args.Out.LocalPath, os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				return errors.Wrapf(err, "failed to open file at path %q", *args.Out.LocalPath)
			}
			defer f.Close()
			out = f
		}

		if err := WriteSpec(out, specContent); err != nil {
			return errors.Wrap(err, "failed to write spec")
		}

		printer.Infof("%s 🎉\n\n", printer.Color.Green("Success!"))
	}

	return nil
}

func pollSpecUntilReady(lc rest.LearnClient, specID akid.APISpecID, enableRelatedFields bool) (string, error) {
	// Fetch the spec, optionally waiting for pending specs to become DONE.
	var spec kgxapi.GetSpecResponse
	getSpecBackoff := &backoff.Backoff{
		Min:    5 * time.Second,
		Max:    5 * time.Minute,
		Factor: 1.2,
		Jitter: true,
	}
	opts := rest.GetSpecOptions{
		EnableRelatedTypes: enableRelatedFields,
	}
	for {
		var err error
		// TODO(kku): make spec download timeout tunable
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		spec, err = lc.GetSpec(ctx, specID, opts)
		cancel()
		if err != nil {
			return "", errors.Wrap(err, "failed to get spec, maybe try increasing --timeout")
		}

		if spec.State != kgxapi.APISpecDone {
			// Backoff and wait for the spec to become ready.
			time.Sleep(getSpecBackoff.Duration())
			printer.Infof("Still working...\n")
		} else {
			break
		}
	}

	return spec.Content, nil
}

func WriteSpec(out io.Writer, spec string) error {
	for len(spec) > 0 {
		if n, err := io.WriteString(out, spec); err != nil {
			return errors.Wrap(err, "failed to write output")
		} else {
			spec = spec[n:]
		}
	}
	return nil
}

func GetSpecURL(domain string, svc akid.ServiceID, spec akid.APISpecID) url.URL {
	specURL := url.URL{
		Scheme: "https",
		Host:   "app." + domain,
		Path:   path.Join("/service", akid.String(svc), "/spec", akid.String(spec)),
	}
	if viper.GetBool("test_only_disable_https") {
		specURL.Scheme = "http"
	}
	return specURL
}

func pathPatternsFromStrings(raws []string) []pp.Pattern {
	patterns := make([]pp.Pattern, 0, len(raws))
	for _, raw := range raws {
		patterns = append(patterns, pp.Parse(raw))
	}
	return patterns
}

func resolveTraceURI(domain string, clientID akid.ClientID, uri akiuri.URI) (akid.LearnSessionID, error) {
	if !uri.ObjectType.IsTrace() {
		return akid.LearnSessionID{}, errors.Errorf("AkitaURI must refer to a trace object")
	}

	frontClient := rest.NewFrontClient(domain, clientID)

	// Resolve service name.
	serviceID, err := util.GetServiceIDByName(frontClient, uri.ServiceName)
	if err != nil {
		return akid.LearnSessionID{}, errors.Wrapf(err, "failed to resolve service name %q", uri.ServiceName)
	}
	learnClient := rest.NewLearnClient(domain, clientID, serviceID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return learnClient.GetLearnSessionIDByName(ctx, uri.ObjectName)
}

// Create a new learn session to house all the local witnesses.
func uploadLocalTraces(domain string, clientID akid.ClientID, svc akid.ServiceID, localPaths []string, includeTrackers bool, plugins []plugin.AkitaPlugin) ([]akid.LearnSessionID, error) {
	learnClient := rest.NewLearnClient(domain, clientID, svc)
	lrns := make([]akid.LearnSessionID, 0, len(localPaths))
	numWitnesses := make([]int, 0, len(localPaths))

	// This is done as a defer so that it runs after all the BackendCollectors have been closed.
	// TODO: refactor single upload into its own function to avoid this?
	defer func() {
		waitForWitnesses(learnClient, svc, lrns, numWitnesses)
	}()

	for _, p := range localPaths {
		// Include the original path in the tags for ease of debugging, and tag the
		// trace as being uploaded.
		traceTags := map[tags.Key]string{
			tags.XAkitaTraceLocalPath: p,
			tags.XAkitaSource:         tags.UploadedSource,
		}

		// The learn session representing the trace is named by the sha256 sum of
		// the trace file.
		checksum, err := sha256FileChecksum(p)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to checksum %s", p)
		}
		checksumStr := base64.URLEncoding.EncodeToString(checksum)

		// If a trace with the same name/checksum already exists, no need to upload
		// a new trace.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if lrn, err := learnClient.GetLearnSessionIDByName(ctx, checksumStr); err == nil {
			printer.Infof("Trace %s (checksum=%s) already exists on Akita Cloud, skipping upload\n", p, checksumStr)
			lrns = append(lrns, lrn)
			continue
		} else {
			var httpErr rest.HTTPError
			if ok := errors.As(err, &httpErr); ok && httpErr.StatusCode != 404 {
				return nil, errors.Wrap(err, "failed to lookup existing traces")
			}
		}

		// Learn session does not exist, create a new learn session.
		lrn, err := util.NewLearnSession(domain, clientID, svc, checksumStr, traceTags, nil)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create backend learn session")
		}

		collector := trace.NewBackendCollector(svc, lrn, learnClient, plugins)
		if !includeTrackers {
			collector = trace.New3PTrackerFilterCollector(collector)
		}
		defer collector.Close()

		witnessCount, err := ProcessHAR(collector, p)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to process HAR file %s", p)
		}
		lrns = append(lrns, lrn)
		numWitnesses = append(numWitnesses, witnessCount)
	}

	return lrns, nil
}

// Wait for the reported number of witnesses in each learn session to match the number uploaded.
// Poll at 5 seconds, 15 seconds, and 30 seconds and give up after three tries.
const numWaitAttempts = 3

var waitDelay [numWaitAttempts]time.Duration = [numWaitAttempts]time.Duration{
	5 * time.Second,
	10 * time.Second,
	15 * time.Second,
}

func waitForWitnesses(learnClient rest.LearnClient, svc akid.ServiceID, sessions []akid.LearnSessionID, numWitnesses []int) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	numWitnessesExpected := 0
	for _, w := range numWitnesses {
		numWitnessesExpected += w
	}

	printer.Infof("Waiting for witness upload to complete...\n")

	var numWitnessesActual int
	var numOK int
	for attempt := 0; attempt < numWaitAttempts; attempt++ {
		time.Sleep(waitDelay[attempt])

		allSessions, err := learnClient.ListLearnSessionsWithStats(ctx, svc, 100)
		if err != nil {
			printer.Errorf("Error listing learn sessions: %v\n", err)
			continue // Try again
		}
		numWitnessesActual = 0
		numOK = 0
		for _, s := range allSessions {
			for j, s2 := range sessions {
				if s.ID == s2 {
					printer.Debugf("%v has %v/%v witnesses\n", akid.String(s.ID), s.Stats.NumWitnesses, numWitnesses[j])
					if s.Stats.NumWitnesses >= numWitnesses[j] {
						numOK += 1
					}
					numWitnessesActual += s.Stats.NumWitnesses
					break
				}
			}
		}
		if numOK == len(sessions) {
			printer.Infof("%d witnesses uploaded.\n", numWitnessesActual)
			return
		}
	}

	printer.Warningf("%d witnesses out of %d uploaded; %d of %d traces complete.\n",
		numWitnessesActual, numWitnessesExpected, numOK, len(sessions))
	printer.Warningf("Continuing with spec creation, but it may be incomplete.\n")
}

func sha256FileChecksum(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}
