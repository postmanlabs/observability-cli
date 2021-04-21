package cloud_client

import "github.com/akitasoftware/akita-libs/akid"

// Indicates that logging has started or stopped for a service. This is added
// to the cloud client's main goroutine when we get responses to our long
// polls.
type LoggingStartStopEvent struct {
	// The service for which logging has started or stopped.
	serviceID akid.ServiceID

	// Specifies how trace events should be logged by the client. Only populated
	// when logging has started for the service.
	LoggingOptions *LoggingOptions
}

func (event LoggingStartStopEvent) handle(client *cloudClient) {
	// Update our logging state.
	serviceInfo := client.serviceInfoByID[event.serviceID]
	serviceInfo.LoggingOptions = event.LoggingOptions

	// Save a local copy of the channels to which we need to propagate the new
	// state, and clear out the channel list. Any future registration requests
	// will be against the newly updated state.
	channelsToSend := serviceInfo.ResponseChannels
	serviceInfo.ResponseChannels = []chan<- ClientLoggingState{}

	// Start a bunch of goroutines to send our responses.
	response := NewClientLoggingState(event.LoggingOptions)
	for _, responseChannel := range channelsToSend {
		go func(responseChannel chan<- ClientLoggingState) {
			defer close(responseChannel)
			responseChannel <- response
		}(responseChannel)
	}

	// Resume long-polling.
	client.longPollService(event.serviceID)
}
