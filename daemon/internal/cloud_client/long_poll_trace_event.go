package cloud_client

import (
	"time"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
)

// Long-polls the cloud to be notified of when a trace becomes inactive.
type longPollTraceEvent struct {
	serviceID akid.ServiceID
	traceID   akid.LearnSessionID
}

func newLongPollTraceEvent(serviceID akid.ServiceID, traceID akid.LearnSessionID) longPollTraceEvent {
	return longPollTraceEvent{
		serviceID: serviceID,
		traceID:   traceID,
	}
}

func (event longPollTraceEvent) handle(client *cloudClient) {
	frontClient := client.frontClient
	go func() {
		err := util.LongPollForTraceDeactivation(frontClient, event.serviceID, event.traceID)
		if err != nil {
			// Log the error, wait a bit, and try again.
			printer.Debugf("Error while polling the trace ID %s: %v\n", akid.String(event.traceID), err)
			time.Sleep(LONG_POLL_INTERVAL)
			client.eventChannel <- event
			return
		}

		// Enqueue an endTraceEvent for the main goroutine to handle.
		client.eventChannel <- newEndTraceEvent(event.serviceID, event.traceID)
	}()
}
