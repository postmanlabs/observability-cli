package cloud_client

import (
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/daemon"
)

// Indicates that logging should start on new traces for a service. This is
// added to the cloud client's main event channel when we get responses to
// longPollServiceEvent.
type startTracesEvent struct {
	// The service for which logging has started or stopped.
	serviceID akid.ServiceID

	// Specifies how trace events should be logged by the client for each new
	// trace.
	loggingOptions []daemon.LoggingOptions
}

func newStartTracesEvent(serviceID akid.ServiceID, loggingOptions []daemon.LoggingOptions) startTracesEvent {
	return startTracesEvent{
		serviceID:      serviceID,
		loggingOptions: loggingOptions,
	}
}

func (event startTracesEvent) handle(client *cloudClient) {
	// Update our set of active traces and start long-polling to be notified of
	// when the traces become inactive.
	serviceInfo := client.serviceInfoByID[event.serviceID]
	for _, loggingOption := range event.loggingOptions {
		client.startTraceEventCollector(event.serviceID, loggingOption)
		client.eventChannel <- newLongPollTraceEvent(event.serviceID, loggingOption.TraceID)
	}

	// Save a local copy of the channels to which we need to propagate the new
	// state, and clear out the channel list. Any future registration requests
	// will be against the newly updated state.
	channelsToSend := serviceInfo.responseChannels
	serviceInfo.responseChannels = []chan<- []daemon.LoggingOptions{}

	// Start a bunch of goroutines to send our responses.
	for _, responseChannel := range channelsToSend {
		go func(responseChannel chan<- []daemon.LoggingOptions) {
			defer close(responseChannel)
			responseChannel <- event.loggingOptions
		}(responseChannel)
	}

	// Resume long-polling for new traces.
	client.eventChannel <- newLongPollServiceEvent(event.serviceID)
}
