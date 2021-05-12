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

// Handles a response from the cloud indicating a change in the set of active
// traces for a service. This method is responsible for resuming long-polling
// for further changes.
//
// This should only be called from within the main goroutine for the cloud
// client.
func (event changedActiveTracesEvent) handle(client *cloudClient) {
	printer.Debugf("Handling changed-traces event for service %s\n", akid.String(event.serviceID))

	serviceInfo, ok := client.serviceInfoByID[event.serviceID]
	if !ok {
		printer.Warningf("Ignoring diff for unknown service: %s\n", akid.String(event.serviceID))
		return
	}

	// Add activated traces.
	for _, loggingOption := range event.activeTraceDiff.ActivatedTraces {
		printer.Infof("Activating trace %s (%s)\n", loggingOption.TraceName, akid.String(loggingOption.TraceID))
		client.startTraceEventCollector(event.serviceID, loggingOption)
	}

	// Handle deactivated traces.
	for _, deactivatedTraceID := range event.activeTraceDiff.DeactivatedTraces {
		traceInfo, ok := serviceInfo.traces[deactivatedTraceID]
		if !ok {
			printer.Debugf("Ignoring deactivation of unknown trace: %s\n", akid.String(deactivatedTraceID))
			continue
		}

		printer.Infof("Deactivating trace %s (%s)\n", traceInfo.loggingOptions.TraceName, akid.String(traceInfo.loggingOptions.TraceID))

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
	serviceInfo.responseChannels = []namedResponseChannel{}

	// Send our responses and register the clients to any activated traces.
	for _, namedChannel := range channelsToSend {
		clientName := namedChannel.clientName
		responseChannel := namedChannel.channel

		defer close(responseChannel)
		responseChannel <- event.activeTraceDiff

		for _, loggingOption := range event.activeTraceDiff.ActivatedTraces {
			serviceInfo := client.serviceInfoByID[loggingOption.ServiceID]
			// Be robust against activating and deactivating in the same diff
			traceInfo, ok := serviceInfo.traces[loggingOption.TraceID]
			if ok {
				traceInfo.clientNames[clientName] = struct{}{}
			} else {
				printer.Warningf("Deactivated a trace that was also activated: %s\n",
					akid.String(loggingOption.TraceID))
			}
		}
	}

	// Resume long-polling for new traces.
	client.eventChannel <- newLongPollServiceEvent(event.serviceID)
}
