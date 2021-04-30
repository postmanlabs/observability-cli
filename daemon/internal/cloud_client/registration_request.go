package cloud_client

import (
	"github.com/akitasoftware/akita-cli/printer"
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
	responseChannel chan<- daemon.ActiveTraceDiff
}

func NewRegistrationRequest(name string, serviceID akid.ServiceID, activeTraces []akid.LearnSessionID, responseChannel chan<- daemon.ActiveTraceDiff) registrationRequest {
	return registrationRequest{
		name:            name,
		serviceID:       serviceID,
		activeTraces:    activeTraces,
		responseChannel: responseChannel,
	}
}

// Instances should only be accessed from within the main goroutine for the
// cloud client.
//
// This should only be called from within the main goroutine for the cloud
// client.
func (req registrationRequest) handle(client *cloudClient) {
	printer.Debugf("Handling poll request for service %q\n", akid.String(req.serviceID))

	// Register the service if it's not already registered.
	serviceInfo := client.ensureServiceRegistered(req.serviceID)

	// Convert the request's list of active traces into a set.
	activeTraces := map[akid.LearnSessionID]struct{}{}
	for _, traceID := range req.activeTraces {
		activeTraces[traceID] = struct{}{}
	}

	// See if the daemon already has a different set of traces than what the
	// client has sent. If so, send back the diff.
	missingTraces := serviceInfo.getActiveTraceDiff(activeTraces)
	if missingTraces.Size() != 0 {
		defer close(req.responseChannel)
		req.responseChannel <- missingTraces
		return
	}

	// Register the client for the eventual response.
	serviceInfo.responseChannels = append(serviceInfo.responseChannels, req.responseChannel)
}
