package util

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	randomdata "github.com/Pallinder/go-randomdata"
	"github.com/google/uuid"
	cache "github.com/patrickmn/go-cache"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/akiuri"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/daemon"
	"github.com/akitasoftware/akita-libs/spec_util"
	"github.com/akitasoftware/akita-libs/tags"
)

var (
	// Maps service name to service ID.
	serviceNameCache = cache.New(30*time.Second, 5*time.Minute)

	// Maps learn session name to ID.
	learnSessionNameCache = cache.New(30*time.Second, 5*time.Minute)

	// Maps postmanCollectionID to ID
	postmanCollectionIDCache = cache.New(30*time.Second, 5*time.Minute)

	serviceIDCache = cache.New(30*time.Second, 5*time.Minute)

	// API timeout
	apiTimeout = 20 * time.Second
)

func NewLearnSession(domain string, clientID akid.ClientID, svc akid.ServiceID, sessionName string, tags map[tags.Key]string, baseSpecRef *kgxapi.APISpecReference) (akid.LearnSessionID, error) {
	learnClient := rest.NewLearnClient(domain, clientID, svc)

	// Create a new learn session.
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	lrn, err := learnClient.CreateLearnSession(ctx, baseSpecRef, sessionName, tags)
	if err != nil {
		return akid.LearnSessionID{}, errors.Wrap(err, "failed to create a new backend trace")
	}

	return lrn, nil
}

func GetServiceIDByName(c rest.FrontClient, name string) (akid.ServiceID, error) {
	// Normalize the name.
	name = strings.ToLower(name)

	if id, found := serviceNameCache.Get(name); found {
		printer.Stderr.Debugf("Cached project name %q is %q\n", name, akid.String(id.(akid.ServiceID)))
		return id.(akid.ServiceID), nil
	}

	// Fill cache.
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	services, err := c.GetServices(ctx)
	if err != nil {
		return akid.ServiceID{}, errors.Wrap(err, "failed to get list of services associated with the account")
	}

	var result akid.ServiceID
	for _, svc := range services {
		if svc.ID == (akid.ServiceID{}) {
			continue
		}

		// Normalize service name.
		svcName := strings.ToLower(svc.Name)
		serviceNameCache.Set(svcName, svc.ID, cache.DefaultExpiration)

		if strings.EqualFold(svc.Name, name) {
			result = svc.ID
			// keep going to fill the cache
		}
	}

	if (result != akid.ServiceID{}) {
		printer.Stderr.Debugf("Project name %q is %q\n", name, akid.String(result))
		return result, nil
	}
	telemetry.Failure("Unknown project ID")
	return akid.ServiceID{}, errors.Errorf("cannot determine project ID for %s", name)
}

func GetServiceNameByServiceID(c rest.FrontClient, serviceID akid.ServiceID) (string, error) {
	unexpectedErrMsg := "Something went wrong while starting the Agent. " +
		"Please contact Postman support (observability-support@postman.com) with the error details"
	failedToGetProjectErrMsg := "Failed to get project for given projectID: %s\n"

	// Check if service is already verified and cached
	if service, found := serviceIDCache.Get(serviceID.String()); found {
		printer.Stderr.Debugf("Cached project %v for projectID %s\n", service, akid.String(serviceID))
		return service.(rest.InsightsService).Name, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	service, err := c.GetService(ctx, serviceID)
	if err != nil {
		httpErr, ok := err.(rest.HTTPError)
		if !ok {
			printer.Stderr.Debugf(failedToGetProjectErrMsg, err)
			return "", errors.Wrap(err, unexpectedErrMsg)
		}

		var errorResponse rest.ErrorResponse
		if err := json.Unmarshal(httpErr.Body, &errorResponse); err != nil {
			printer.Stderr.Debugf(failedToGetProjectErrMsg, err)
			return "", errors.Wrap(err, unexpectedErrMsg)
		}

		if httpErr.StatusCode == 404 {
			//lint:ignore ST1005 This is a user-facing error message
			return "", fmt.Errorf("There is no project with given ID %s. Ensure that your projectID is correct", serviceID)
		} else if httpErr.StatusCode == 403 {
			//lint:ignore ST1005 This is a user-facing error message
			return "", fmt.Errorf("You cannot send traffic to the project with ID %s. "+
				"Ensure that your projectID is correct and that you have required permissions. "+
				"If you do not have required permissions, please contact the workspace administrator", serviceID)
		}

		return "", errors.Wrap(err, unexpectedErrMsg)
	}

	serviceIDCache.Set(serviceID.String(), service, cache.DefaultExpiration)

	return service.Name, nil
}

func GetServiceIDByPostmanCollectionID(c rest.FrontClient, ctx context.Context, collectionID string) (akid.ServiceID, error) {
	services, err := c.GetServices(ctx)
	if err != nil {
		return akid.ServiceID{}, err
	}

	var result akid.ServiceID
	for _, svc := range services {
		if svc.ID == (akid.ServiceID{}) {
			continue
		}

		if svc.PostmanMetaData == (rest.PostmanMetaData{}) {
			continue
		}

		// Normalize collectionID.
		svcCollectionID := strings.ToLower(svc.PostmanMetaData.CollectionID)

		if strings.EqualFold(collectionID, svcCollectionID) {
			result = svc.ID
		}
	}

	return result, nil
}

func GetOrCreateServiceIDByPostmanCollectionID(c rest.FrontClient, collectionID string) (akid.ServiceID, error) {
	// Normalize the collectionID.
	collectionID = strings.ToLower(collectionID)
	unexpectedErrMsg := "Something went wrong while starting the Agent. " +
		"Please contact Postman support (observability-support@postman.com) with the error details"
	failedToCreateProjectErrMsg := "Failed to create project for given collectionID: %s\n"

	if id, found := postmanCollectionIDCache.Get(collectionID); found {
		printer.Stderr.Debugf("Cached collectionID %q is %q\n", collectionID, akid.String(id.(akid.ServiceID)))
		return id.(akid.ServiceID), nil
	}

	// Fetch service and fill cache
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	serviceID, err := GetServiceIDByPostmanCollectionID(c, ctx, collectionID)
	if err != nil {
		printer.Stderr.Debugf("Failed to get list of projects associated with the API Key: %s\n", err)
		return akid.ServiceID{}, errors.Wrap(err, unexpectedErrMsg)
	}

	if (serviceID != akid.ServiceID{}) {
		printer.Stderr.Debugf("ProjectID for given Postman collectionID %q is %q\n", collectionID, serviceID)
		postmanCollectionIDCache.Set(collectionID, serviceID, cache.DefaultExpiration)
		return serviceID, nil
	}

	name := postmanRandomName()
	printer.Debugf("Found no project for given collectionID: %s, creating a new project %q\n", collectionID, name)
	// Create service for given postman collectionID
	resp, err := c.CreateService(ctx, name, collectionID)
	if err != nil {
		httpErr, ok := err.(rest.HTTPError)
		if !ok {
			printer.Stderr.Debugf(failedToCreateProjectErrMsg, err)
			return akid.ServiceID{}, errors.Wrap(err, unexpectedErrMsg)
		}

		var errorResponse rest.ErrorResponse
		if err := json.Unmarshal(httpErr.Body, &errorResponse); err != nil {
			printer.Stderr.Debugf(failedToCreateProjectErrMsg, err)
			return akid.ServiceID{}, errors.Wrap(err, unexpectedErrMsg)
		}

		if httpErr.StatusCode == 409 && errorResponse.Message == "collection_already_mapped" {
			serviceID, err := GetServiceIDByPostmanCollectionID(c, ctx, collectionID)
			if err != nil {
				printer.Stderr.Debugf(failedToCreateProjectErrMsg, err)
				return akid.ServiceID{}, errors.Wrap(err, unexpectedErrMsg)
			}

			if (serviceID != akid.ServiceID{}) {
				printer.Stderr.Debugf("ProjectID for Postman collectionID %q is %q\n", collectionID, serviceID)
				postmanCollectionIDCache.Set(collectionID, serviceID, cache.DefaultExpiration)
				return serviceID, nil
			}

		} else if httpErr.StatusCode == 403 {
			//lint:ignore ST1005 This is a user-facing error message
			error := fmt.Errorf("you cannot send traffic to the collection with ID %s. "+
				"Ensure that your collection ID is correct and that you have edit permissions on the collection. "+
				"If you do not have edit permissions, please contact the workspace administrator to add you as a collection editor.", collectionID)
			return akid.ServiceID{}, error
		}

		return akid.ServiceID{}, errors.Wrap(err, unexpectedErrMsg)
	}

	printer.Debugf("Got projectID %s\n", resp.ResourceID)
	postmanCollectionIDCache.Set(collectionID, resp.ResourceID, cache.DefaultExpiration)

	return resp.ResourceID, nil
}

func DaemonHeartbeat(c rest.FrontClient, daemonName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	err := c.DaemonHeartbeat(ctx, daemonName)
	if err != nil {
		return errors.Wrap(err, "failed to send daemon heartbeat")
	}
	return nil
}

// Long-polls the cloud for changes to the set of active traces for a service.
func LongPollActiveTracesForService(c rest.FrontClient, daemonName string, serviceID akid.ServiceID, currentTraces []akid.LearnSessionID) (daemon.ActiveTraceDiff, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	return c.LongPollActiveTracesForService(ctx, daemonName, serviceID, currentTraces)
}

func GetLearnSessionIDByName(c rest.LearnClient, name string) (akid.LearnSessionID, error) {
	if id, found := learnSessionNameCache.Get(name); found {
		return id.(akid.LearnSessionID), nil
	}

	// Fill cache.
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	id, err := c.GetLearnSessionIDByName(ctx, name)
	if err != nil {
		return id, errors.Wrapf(err, "cannot determine learn session ID for %s", name)
	}
	learnSessionNameCache.Set(name, id, cache.DefaultExpiration)
	return id, nil
}

func GetLearnSessionByTags(c rest.LearnClient, serviceID akid.ServiceID, tags map[tags.Key]string) (*kgxapi.ListedLearnSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	sessions, err := c.ListLearnSessions(ctx, serviceID, tags, 250, 0)
	if err != nil {
		return nil, errors.Wrapf(err, "listing sessions for %v by tag failed", akid.String(serviceID))
	}

	if len(sessions) == 0 {
		return nil, nil
	}

	// TODO: support AND-based matching on the back-end
	printer.Debugf("Found %d sessions, filtering for most recent match.\n", len(sessions))
	latest := -1
	for i, s := range sessions {
		if latest < 0 || s.CreationTime.After(sessions[latest].CreationTime) {
			latest = i
		}
	}

	return sessions[latest], nil
}

// Get the most recent trace for a service; helper method for commands to implement the
// --append-by-tag or --trace-tag flags.
func GetTraceURIByTags(domain string, clientID akid.ClientID, serviceName string, tags map[tags.Key]string, flagName string) (akiuri.URI, error) {
	if len(tags) == 0 {
		return akiuri.URI{}, fmt.Errorf("Must specify a tag to match with %q", flagName)
	}

	if len(tags) > 1 {
		return akiuri.URI{}, fmt.Errorf("%q currently supports only a single tag", flagName)
	}

	// Resolve ServiceID
	// TODO: find a better way to overlap this with commands that already do the lookup
	frontClient := rest.NewFrontClient(domain, clientID)
	serviceID, err := GetServiceIDByName(frontClient, serviceName)
	if err != nil {
		return akiuri.URI{}, errors.Wrapf(err, "failed to resolve project name %q", serviceName)
	}

	learnClient := rest.NewLearnClient(domain, clientID, serviceID)
	learnSession, err := GetLearnSessionByTags(learnClient, serviceID, tags)
	if err != nil {
		return akiuri.URI{}, errors.Wrapf(err, "failed to list traces for %q", serviceName)
	}
	if learnSession == nil {
		printer.Infof("No traces matching specified tag\n")
		return akiuri.URI{
			ServiceName: serviceName,
			ObjectName:  "", // create a new name
			ObjectType:  akiuri.TRACE.Ptr(),
		}, nil
	}
	uri := akiuri.URI{
		ServiceName: serviceName,
		ObjectName:  learnSession.Name,
		ObjectType:  akiuri.TRACE.Ptr(),
	}
	printer.Infof("Trace %v matches tag\n", uri.String())
	return uri, nil
}

func ResolveSpecURI(lc rest.LearnClient, uri akiuri.URI) (akid.APISpecID, error) {
	if !uri.ObjectType.IsSpec() {
		return akid.APISpecID{}, errors.Errorf("AkitaURI must refer to a spec object")
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	return lc.GetAPISpecIDByName(ctx, uri.ObjectName)
}

// Adjective and Noun are up to 11 characters each
// Random hex = 8 characters
// Separators = 2 characters
// Up to 32 characters, which is the maximum supported.
func randomName() string {
	return strings.Join([]string{
		randomdata.Adjective(),
		randomdata.Noun(),
		uuid.New().String()[0:8],
	}, "-")
}

// Adjective: 11 characters
// Random hex = 8 characters
// "pm" and separators = 5 characters
// Leaves up to 8 characters for the name
func postmanRandomName() string {
	noun := randomdata.Noun()
	if len(noun) > 8 {
		noun = noun[0:8]
	}
	return strings.Join([]string{
		randomdata.Adjective(),
		noun,
		"pm",
		uuid.New().String()[0:8],
	}, "-")
}

// Produces a random name for a learning session.
var RandomLearnSessionName func() string = randomName

// Produces a random name for an API model.
var RandomAPIModelName func() string = randomName

// Detect Akita internal traffic
func ContainsCLITraffic(t akinet.ParsedNetworkTraffic) bool {
	var header http.Header
	switch tc := t.Content.(type) {
	case akinet.HTTPRequest:
		header = tc.Header
	case akinet.HTTPResponse:
		header = tc.Header
	default:
		return false
	}

	for _, k := range []string{spec_util.XAkitaCLIGitVersion, spec_util.XAkitaRequestID} {
		if header.Get(k) != "" {
			return true
		}
	}
	return false
}

func ParseTags(tagsArg []string) (map[tags.Key]string, error) {
	tagSet, err := tags.FromPairs(tagsArg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse tags")
	}
	return tagSet, nil
}

func ParseTagsAndWarn(tagsArg []string) (map[tags.Key]string, error) {
	tagSet, err := tags.FromPairs(tagsArg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse tags")
	}
	WarnOnReservedTags(tagSet)
	return tagSet, nil
}

func WarnOnReservedTags(tagSet map[tags.Key]string) {
	for t, _ := range tagSet {
		if tags.IsReservedKey(t) {
			printer.Warningf("%s is an Akita-reserved key. Its value may be overwritten internally\n", t)
		}
	}
}
