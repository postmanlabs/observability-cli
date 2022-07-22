package util

import (
	"context"
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
		printer.Stderr.Debugf("Service name %q is %q\n", name, akid.String(result))
		return result, nil
	}
	return akid.ServiceID{}, errors.Errorf("cannot determine project ID for %s", name)
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

	sessions, err := c.ListLearnSessions(ctx, serviceID, tags)
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

func randomName() string {
	return strings.Join([]string{
		randomdata.Adjective(),
		randomdata.Noun(),
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
