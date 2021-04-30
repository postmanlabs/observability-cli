package cloud_client

import (
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/daemon"
)

// Indicates that the set of active traces for a service has changed. This is
// added to the cloud client's main event channel when we get responses to
// longPollServiceEvent.
type changedActiveTracesEvent struct {
	// The service for which the set of active traces has changed.
	serviceID akid.ServiceID

	// Specifies which traces have been activated, and which have been
	// deactivated.
	activeTraceDiff daemon.ActiveTraceDiff
}

func newChangeActiveTracesEvent(serviceID akid.ServiceID, activeTraceDiff daemon.ActiveTraceDiff) changedActiveTracesEvent {
	return changedActiveTracesEvent{
		serviceID:       serviceID,
		activeTraceDiff: activeTraceDiff,
	}
}

func (event changedActiveTracesEvent) handle(client *cloudClient) {
	printer.Debugf("Handling changed-traces event for service %s\n", akid.String(event.serviceID))

	serviceInfo, ok := client.serviceInfoByID[event.serviceID]
	if !ok {
		printer.Debugf("Ignoring diff for unknown service: %s\n", akid.String(event.serviceID))
		return
	}

	// Add activated traces.
	for _, loggingOption := range event.activeTraceDiff.ActivatedTraces {
		client.startTraceEventCollector(event.serviceID, loggingOption)
	}

	// Handle deactivated traces.
	for _, deactivatedTraceID := range event.activeTraceDiff.DeactivatedTraces {
		traceInfo, ok := serviceInfo.traces[deactivatedTraceID]
		if !ok {
			printer.Debugf("Ignoring deactivation of unknown trace: %s\n", akid.String(deactivatedTraceID))
			continue
		}

		traceInfo.active = false

		if len(traceInfo.clientNames) > 0 {
			continue
		}

		// No clients are registered for the deactivated trace. Unregister the
		// trace and close the event channel.
		client.unregisterTrace(event.serviceID, deactivatedTraceID)
	}

	// Save a local copy of the channels to which we need to propagate the new
	// state, and clear out the channel list. Any future registration requests
	// will be against the newly updated state.
	channelsToSend := serviceInfo.responseChannels
	serviceInfo.responseChannels = []chan<- daemon.ActiveTraceDiff{}

	// Start a bunch of goroutines to send our responses.
	for _, responseChannel := range channelsToSend {
		go func(responseChannel chan<- daemon.ActiveTraceDiff) {
			defer close(responseChannel)
			responseChannel <- event.activeTraceDiff
		}(responseChannel)
	}

	// Resume long-polling for new traces.
	client.eventChannel <- newLongPollServiceEvent(event.serviceID)
}
