package util

import (
	"context"
	"strings"
	"time"

	randomdata "github.com/Pallinder/go-randomdata"
	"github.com/google/uuid"
	cache "github.com/patrickmn/go-cache"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/daemon"
)

var (
	// Maps service name to service ID.
	serviceNameCache = cache.New(30*time.Second, 5*time.Minute)

	// Maps learn session name to ID.
	learnSessionNameCache = cache.New(30*time.Second, 5*time.Minute)
)

func NewLearnSession(domain string, clientID akid.ClientID, svc akid.ServiceID, sessionName string, tags map[string]string, baseSpecRef *kgxapi.APISpecReference) (akid.LearnSessionID, error) {
	learnClient := rest.NewLearnClient(domain, clientID, svc)

	// Create a new learn session.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lrn, err := learnClient.CreateLearnSession(ctx, baseSpecRef, sessionName, tags)
	if err != nil {
		return akid.LearnSessionID{}, errors.Wrap(err, "failed to create a new backend trace")
	}

	return lrn, nil
}

func GetServiceIDByName(c rest.FrontClient, name string) (akid.ServiceID, error) {
	if id, found := serviceNameCache.Get(name); found {
		return id.(akid.ServiceID), nil
	}

	// Fill cache.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	services, err := c.GetServices(ctx)
	if err != nil {
		return akid.ServiceID{}, errors.Wrap(err, "failed to get list of services associated with the account")
	}

	for _, svc := range services {
		if svc.ID == (akid.ServiceID{}) {
			continue
		}
		serviceNameCache.Set(svc.Name, svc.ID, cache.DefaultExpiration)

		if svc.Name == name {
			return svc.ID, nil
		}
	}

	return akid.ServiceID{}, errors.Errorf("cannot determine service ID for %s", name)
}

func DaemonHeartbeat(c rest.FrontClient, daemonName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.DaemonHeartbeat(ctx, daemonName)
	if err != nil {
		return errors.Wrap(err, "failed to send daemon heartbeat")
	}
	return nil
}

// Long-polls the cloud for additions to the set of active traces for a
// service.
func LongPollActiveTracesForService(c rest.FrontClient, serviceID akid.ServiceID, currentTraces []akid.LearnSessionID) ([]daemon.LoggingOptions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	return c.LongPollActiveTracesForService(ctx, serviceID, currentTraces)
}

// Long-polls the cloud for the deactivation of a trace.
func LongPollForTraceDeactivation(c rest.FrontClient, serviceID akid.ServiceID, traceID akid.LearnSessionID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	return c.LongPollForTraceDeactivation(ctx, serviceID, traceID)
}

func GetLearnSessionIDByName(c rest.LearnClient, name string) (akid.LearnSessionID, error) {
	if id, found := learnSessionNameCache.Get(name); found {
		return id.(akid.LearnSessionID), nil
	}

	// Fill cache.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := c.GetLearnSessionIDByName(ctx, name)
	if err != nil {
		return id, errors.Wrapf(err, "cannot determine learn session ID for %s", name)
	}
	learnSessionNameCache.Set(name, id, cache.DefaultExpiration)
	return id, nil
}

func ResolveSpecURI(lc rest.LearnClient, uri akiuri.URI) (akid.APISpecID, error) {
	if !uri.ObjectType.IsSpec() {
		return akid.APISpecID{}, errors.Errorf("AkitaURI must refer to a spec object")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
