package cloud_client

import (
	"time"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
)

// Long-polls the cloud for updates to a service.
type longPollServiceEvent struct {
	serviceID akid.ServiceID
}

func newLongPollServiceEvent(serviceID akid.ServiceID) longPollServiceEvent {
	return longPollServiceEvent{
		serviceID: serviceID,
	}
}

func (event longPollServiceEvent) handle(client *cloudClient) {
	currentTraces := client.getCurrentTraces(event.serviceID)
	frontClient := client.frontClient
	go func() {
		// Send a request to the cloud containing a list of the traces currently
		// being logged. The response will be a list of new traces to log.
		newTraces, err := util.LongPollActiveTracesForService(frontClient, event.serviceID, currentTraces)
		if err != nil {
			// Log the error, wait a bit, and try again.
			printer.Debugf("Error while polling service ID %s: %v\n", akid.String(event.serviceID), err)
			time.Sleep(LONG_POLL_INTERVAL)
			client.eventChannel <- event
			return
		}

		// Enqueue a startTracesEvent for the main goroutine to handle.
		client.eventChannel <- newStartTracesEvent(event.serviceID, newTraces)
	}()
}
