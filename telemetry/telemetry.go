package telemetry

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"sync"
	"time"

	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/version"
	"github.com/akitasoftware/akita-libs/analytics"
)

var (
	// Shared client object
	analyticsClient analytics.Client = nullClient{}

	// Is analytics enabled?
	analyticsEnabled bool

	// Client key; set at link-time with -X flag
	defaultSegmentKey = ""

	// Store the distinct ID; run through the process
	// of getting it only once.
	userDistinctID   string
	userDistinctOnce sync.Once

	// Timeout talking to API.
	// Shorter than normal because we don't want the CLI to be slow.
	userAPITimeout = 2 * time.Second
)

type nullClient struct{}

func (_ nullClient) TrackEvent(_ *analytics.Event) error {
	return nil
}

func (_ nullClient) Track(distinctID string, name string, properties map[string]any) error {
	return nil
}

func (_ nullClient) Close() error {
	return nil
}

// Initialize the telemetry client.
// This should be called once at startup either from the root command or from a subcommand that overrides the default PersistentPreRun.
func Init(isLoggingEnabled bool) {
	// Opt-out mechanism
	disableTelemetry := os.Getenv("AKITA_DISABLE_TELEMETRY")
	if disableTelemetry != "" {
		if val, err := strconv.ParseBool(disableTelemetry); err == nil && val {
			printer.Infof("Telemetry disabled via opt-out.\n")
			analyticsClient = nullClient{}
			return
		}
	}

	// If unset, will be "" and we'll use the default
	segmentEndpoint := os.Getenv("AKITA_SEGMENT_ENDPOINT")

	// If unset, will use this hard-coded value.
	segmentKey := os.Getenv("AKITA_SEGMENT_WRITE_KEY")
	if segmentKey == "" {
		segmentKey = defaultSegmentKey
	}
	if segmentKey == "" {
		if isLoggingEnabled {
			printer.Infof("Telemetry unavailable; no Segment key configured.\n")
			printer.Infof("This is caused by building from source rather than using an official build.\n")
		}
		analyticsClient = nullClient{}
		return
	}

	var err error
	analyticsClient, err = analytics.NewClient(
		analytics.Config{
			WriteKey:        segmentKey,
			SegmentEndpoint: segmentEndpoint,
			App: analytics.AppInfo{
				Name:      "akita-cli",
				Version:   version.ReleaseVersion().String(),
				Build:     version.GitVersion(),
				Namespace: "",
			},
			// No output from the Segment library
			IsLoggingEnabled: false,
			// IsMixpanelEnabled: false,  -- irrelevant for us, leaving at default value
			BatchSize: 1, // disable batching
		},
	)
	if err != nil {
		if isLoggingEnabled {
			printer.Infof("Telemetry unavailable; error setting up Segment client: %v\n", err)
			printer.Infof("Akita support will not be able to see any errors you encounter.\n")
			printer.Infof("Please send this log message to observability-support@postman.com.\n")
		}
		analyticsClient = nullClient{}
	} else {
		analyticsEnabled = true
	}
}

func getDistinctID() string {
	// If we have a user email, use that!
	// Otherwise use the configured API Key.
	// Failing that, try to use the user name and host name?

	id := os.Getenv("AKITA_SEGMENT_DISTINCT_ID")
	if id != "" {
		return id
	}

	// If there's no credentials configured, skip the API call and
	// do not emit a log message.
	// Similarly if telemetry is disabled.
	key, secret := cfg.GetAPIKeyAndSecret()
	if key != "" && secret != "" && analyticsEnabled {
		// Call the REST API to get the user email associated with the configured
		// API key.
		ctx, cancel := context.WithTimeout(context.Background(), userAPITimeout)
		defer cancel()
		frontClient := rest.NewFrontClient(rest.Domain, GetClientID())
		userResponse, err := frontClient.GetUser(ctx)
		if err == nil {
			if userResponse.Email != "" {
				return userResponse.Email
			}

			// Use the user ID if no email is present;
			// this should be fixed in the current backend.
			return userResponse.ID.String()
		}

		printer.Infof("Telemetry using temporary ID; /v1/user API call failed: %v\n", err)
		printer.Infof("This error may indicate a problem communicating with the Akita servers,\n")
		printer.Infof("but the agent will still attempt to send telemetry Akita support.\n")
	}

	if key != "" {
		return key
	}

	localUser, err := user.Current()
	if err != nil {
		return "unknown"
	}
	localHost, err := os.Hostname()
	if err != nil {
		return localUser.Username
	}
	return localUser.Username + "@" + localHost
}

func distinctID() string {
	userDistinctOnce.Do(func() {
		userDistinctID = getDistinctID()

		// Set up automatic reporting of all API errors
		// (rest can't call telemetry directly because we call rest above!)
		rest.SetAPIErrorHandler(APIError)

		printer.Debugf("Using ID %q for telemetry\n", userDistinctID)
	})
	return userDistinctID
}

// Report an error in a particular operation (inContext), including
// the text of the error.
func Error(inContext string, e error) {
	analyticsClient.Track(distinctID(),
		fmt.Sprintf("Error in %s", inContext),
		map[string]any{
			"error": e.Error(),
			"type":  "error",
		},
	)
}

type eventRecord struct {
	// Number of events since the last one was sent
	Count int

	// Next time to send an event
	NextSend time.Time
}

var rateLimitMap sync.Map

const rateLimitDuration = 60 * time.Second

// Report an error in a particular operation (inContext), including
// the text of the error.  Send only one trace event per minute for
// this particular context; count the remainder.
//
// Rate-limited errors are not flushed when telemetry is shut down.
//
// TODO: consider using the error too, but that could increase
// the cardinality of the map by a lot.
func RateLimitError(inContext string, e error) {
	newRecord := eventRecord{
		Count:    0,
		NextSend: time.Now().Add(rateLimitDuration),
	}
	existing, present := rateLimitMap.LoadOrStore(inContext, newRecord)

	count := 1
	if present {
		record := existing.(eventRecord)

		if record.NextSend.After(time.Now()) {
			// This is a data race but not worth worrying about
			// (by using a mutex); sometimes the count will be low.
			record.Count += 1
			rateLimitMap.Store(inContext, record)
			return
		}

		// Time to send a new record, reset the count back to 0 and send
		// the count of the backlog, plus the current event.
		count = record.Count + 1
		rateLimitMap.Store(inContext, newRecord)
	}

	analyticsClient.Track(distinctID(),
		fmt.Sprintf("Error in %s", inContext),
		map[string]any{
			"error": e.Error(),
			"type":  "error",
			"count": count,
		},
	)
}

// Report an error in a particular API, including the text of the error.
func APIError(method string, path string, e error) {
	analyticsClient.Track(distinctID(),
		fmt.Sprintf("Error calling API"),
		map[string]any{
			"method": method,
			"path":   path,
			"error":  e.Error(),
			"type":   "error",
		},
	)
}

// Report a failure without a specific error object
func Failure(message string) {
	analyticsClient.Track(distinctID(),
		message,
		map[string]any{
			"type": "error",
		},
	)
}

// Report success of an operation
func Success(message string) {
	analyticsClient.Track(distinctID(),
		message,
		map[string]any{
			"type": "success",
		},
	)
}

// Report a step in a multi-part workflow.
func WorkflowStep(workflow string, message string) {
	analyticsClient.Track(distinctID(),
		message,
		map[string]any{
			"type":     "workflow",
			"workflow": workflow,
		},
	)
}

// Report command line flags (before any error checking.)
func CommandLine(command string, commandLine []string) {
	analyticsClient.Track(distinctID(),
		fmt.Sprintf("Executed %s", command),
		map[string]any{
			"command_line": commandLine,
		},
	)
}

// Report the platform and version of an attempted integration
func InstallIntegrationVersion(integration, arch, platform, version string) {
	analyticsClient.Track(distinctID(),
		fmt.Sprintf("Install %s", integration),
		map[string]any{
			"architecture": arch,
			"version":      version,
			"platform":     platform,
		},
	)
}

// Flush the telemetry to its endpoint
// (even buffer size of 1 is not enough if the CLi exits right away.)
func Shutdown() {
	err := analyticsClient.Close()
	if err != nil {
		printer.Stderr.Errorf("Error flushing telemetry: %v\n", err)
		printer.Infof("Akita support may not be able to see the last error message you received.\n")
		printer.Infof("Please send the CLI output to observability-support@postman.com.\n")
	}
}
