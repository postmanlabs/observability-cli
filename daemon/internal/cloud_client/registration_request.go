package cloud_client

import (
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/daemon"
)

// A request for registering a client (middleware) to the daemon.
type registrationRequest struct {
	// The name of the client.
	name string

	// The service with which the client is associated.
	serviceID akid.ServiceID

	// The set of traces for which the client is actively logging.
	activeTraces []akid.LearnSessionID

	// The channel on which to send the response to this request.
	responseChannel chan<- []daemon.LoggingOptions
}

func NewRegistrationRequest(name string, serviceID akid.ServiceID, activeTraces []akid.LearnSessionID, responseChannel chan<- []daemon.LoggingOptions) registrationRequest {
	return registrationRequest{
		name:            name,
		serviceID:       serviceID,
		activeTraces:    activeTraces,
		responseChannel: responseChannel,
	}
}

func (req registrationRequest) handle(client *cloudClient) {
	// Register the service if it's not already registered.
	serviceInfo := client.ensureServiceRegistered(req.serviceID)

	// Covert the request's list of active traces into a set.
	activeTraces := map[akid.LearnSessionID]struct{}{}
	for _, traceID := range req.activeTraces {
		activeTraces[traceID] = struct{}{}
	}

	// See if the daemon knows about traces that the client is missing.
	missingTraces := []daemon.LoggingOptions{}
	for traceID, traceInfo := range serviceInfo.traces {
		if _, ok := activeTraces[traceID]; !ok {
			missingTraces = append(missingTraces, traceInfo.loggingOptions)
		}
	}

	// If the client is missing any traces, send them back.
	if len(missingTraces) != 0 {
		defer close(req.responseChannel)
		req.responseChannel <- missingTraces
		return
	}

	// Register the client for the eventual response.
	serviceInfo.responseChannels = append(serviceInfo.responseChannels, req.responseChannel)
}
