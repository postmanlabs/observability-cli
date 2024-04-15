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
	"github.com/akitasoftware/akita-cli/consts"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/version"
	"github.com/akitasoftware/akita-libs/analytics"
	"github.com/akitasoftware/go-utils/maps"
)

var (
	// Shared client object
	analyticsClient analytics.Client = nullClient{}

	// Is analytics enabled?
	analyticsEnabled bool

	// Client key; set at link-time with -X flag
	defaultAmplitudeKey = ""

	// Store the user ID and team ID; run through the process
	// of getting it only once.
	userID           string
	teamID           string
	userIdentityOnce sync.Once

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
	disableTelemetry := os.Getenv("AKITA_DISABLE_TELEMETRY") + os.Getenv("POSTMAN_INSIGHTS_AGENT_DISABLE_TELEMETRY")
	if disableTelemetry != "" {
		if val, err := strconv.ParseBool(disableTelemetry); err == nil && val {
			printer.Infof("Telemetry disabled via opt-out.\n")
			analyticsClient = nullClient{}
			return
		}
	}

	// If unset, will be "" and we'll use the default
	amplitudeEndpoint := os.Getenv("POSTMAN_INSIGHTS_AGENT_AMPLITUDE_ENDPOINT")

	// If unset, will use this hard-coded value.
	amplitudeKey := os.Getenv("POSTMAN_INSIGHTS_AGENT_AMPLITUDE_WRITE_KEY")
	if amplitudeKey == "" {
		amplitudeKey = defaultAmplitudeKey
	}
	if amplitudeKey == "" {
		if isLoggingEnabled {
			printer.Infof("Telemetry unavailable; no Amplitude key configured.\n")
			printer.Infof("This is caused by building from source rather than using an official build.\n")
		}
		analyticsClient = nullClient{}
		return
	}

	var err error
	analyticsClient, err = analytics.NewClient(
		analytics.Config{
			// Enable analytics for Amplitude only
			IsAmplitudeEnabled: true,
			AmplitudeConfig: analytics.AmplitudeConfig{
				AmplitudeAPIKey:   amplitudeKey,
				AmplitudeEndpoint: amplitudeEndpoint,
				// No output from the Amplitude library
				IsLoggingEnabled: false,
			},
			App: analytics.AppInfo{
				Name:      "akita-cli",
				Version:   version.ReleaseVersion().String(),
				Build:     version.GitVersion(),
				Namespace: "",
			},
		},
	)
	if err != nil {
		if isLoggingEnabled {
			printer.Infof("Telemetry unavailable; error setting up Analytics(Amplitude) client: %v\n", err)
			printer.Infof("Postman support will not be able to see any errors you encounter.\n")
			printer.Infof("Please send this log message to %s.\n", consts.SupportEmail)
		}
		analyticsClient = nullClient{}
		return
	}

	userID, teamID, err = getUserIdentity() // Initialize user ID and team ID
	if err != nil {
		if isLoggingEnabled {
			printer.Infof("Telemetry unavailable; error getting userID for given API key: %v\n", err)
			printer.Infof("Postman support will not be able to see any errors you encounter.\n")
			printer.Infof("Please send this log message to %s.\n", consts.SupportEmail)
		}
		analyticsClient = nullClient{}
		return
	}

	// Set up automatic reporting of all API errors
	// (rest can't call telemetry directly because we call rest above!)
	rest.SetAPIErrorHandler(APIError)

	analyticsEnabled = true
}

func getUserIdentity() (string, string, error) {
	// If we can get user details use userID and teamID
	// Otherwise use the configured API Key.
	// Failing that, try to use the user name and host name.
	// In latter 2 cases teamID will be empty.

	id := os.Getenv("POSTMAN_ANALYTICS_DISTINCT_ID")
	if id != "" {
		return id, "", nil
	}

	// If there's no credentials configured, skip the API call and
	// do not emit a log message.
	// Similarly if telemetry is disabled.
	if cfg.CredentialsPresent() && analyticsEnabled {
		// Call the REST API to get the postman user associated with the configured
		// API key.
		ctx, cancel := context.WithTimeout(context.Background(), userAPITimeout)
		defer cancel()
		frontClient := rest.NewFrontClient(rest.Domain, GetClientID())
		userResponse, err := frontClient.GetUser(ctx)
		if err == nil {
			return fmt.Sprint(userResponse.ID), fmt.Sprint(userResponse.TeamID), nil
		}

		printer.Infof("Telemetry using temporary ID; GetUser API call failed: %v\n", err)
		printer.Infof("This error may indicate a problem communicating with the Postman servers,\n")
		printer.Infof("but the agent will still attempt to send telemetry to Postman support.\n")
	}

	// Try to derive a distinct ID from the credentials, if present, even
	// if the getUser() call failed.
	keyID := cfg.DistinctIDFromCredentials()
	if keyID != "" {
		return keyID, "", nil
	}

	localUser, err := user.Current()
	if err != nil {
		return "", "", err
	}
	localHost, err := os.Hostname()
	if err != nil {
		return localUser.Username, "", nil
	}
	return localUser.Username + "@" + localHost, "", nil
}

// Report an error in a particular operation (inContext), including
// the text of the error.
func Error(inContext string, e error) {
	tryTrackingEvent(
		"Operation - Errored",
		map[string]any{
			"operation": inContext,
			"error":     e.Error(),
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

	tryTrackingEvent(
		"Operation - Rate Limited",
		map[string]any{
			"operation": inContext,
			"error":     e.Error(),
			"count":     count,
		},
	)
}

// Report an error in a particular API, including the text of the error.
func APIError(method string, path string, e error) {
	tryTrackingEvent(
		"API Call - Errored",
		map[string]any{
			"method": method,
			"path":   path,
			"error":  e.Error(),
		},
	)
}

// Report a failure without a specific error object
func Failure(message string) {
	tryTrackingEvent(
		"Operation - Errored",
		map[string]any{
			"error": message,
		},
	)
}

// Report success of an operation
func Success(message string) {
	tryTrackingEvent(
		"Operation - Succeeded",
		map[string]any{
			"operation": message,
		},
	)
}

// Report a step in a multi-part workflow.
func WorkflowStep(workflow string, message string) {
	tryTrackingEvent(
		"Workflow Step - Executed",
		map[string]any{
			"step":     message,
			"workflow": workflow,
		},
	)
}

// Report command line flags (before any error checking.)
func CommandLine(command string, commandLine []string) {
	tryTrackingEvent(
		"Command - Executed",
		map[string]any{
			"command":      command,
			"command_line": commandLine,
		},
	)
}

// Report the platform and version of an attempted integration
func InstallIntegrationVersion(integration, arch, platform, version string) {
	tryTrackingEvent(
		"Integration - Installed",
		map[string]any{
			"integration":  integration,
			"architecture": arch,
			"version":      version,
			"platform":     platform,
		})
}

// Flush the telemetry to its endpoint
// (even buffer size of 1 is not enough if the CLi exits right away.)
func Shutdown() {
	err := analyticsClient.Close()
	if err != nil {
		printer.Stderr.Errorf("Error flushing telemetry: %v\n", err)
		printer.Infof("Postman support may not be able to see the last error message you received.\n")
		printer.Infof("Please send the CLI output to %s.\n", consts.SupportEmail)
	}
}

// Attempts to track an event using the provided event name and properties.
// It initializes the user identity, adds the user ID and team ID to the event properties,
// and then sends the event to the analytics client.
// If there is an error sending the event, a warning message is printed.
func tryTrackingEvent(eventName string, eventProperties maps.Map[string, any]) {
	eventProperties.Upsert("user_id", userID, func(v, newV any) any { return v })

	if teamID != "" {
		eventProperties.Upsert("team_id", teamID, func(v, newV any) any { return v })
	}

	err := analyticsClient.Track(userID, eventName, eventProperties)
	if err != nil {
		printer.Warningf("Error sending analytics event %q: %v\n", eventName, err)
	}
}
