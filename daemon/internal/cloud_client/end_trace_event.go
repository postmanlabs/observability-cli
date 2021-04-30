package cloud_client

import (
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/akid"
)

// Indicates that logging should stop on a trace. This is added to the cloud
// client's main event channel when we get responses to longPollTraceEvent.
type endTraceEvent struct {
	serviceID akid.ServiceID
	traceID   akid.LearnSessionID
}

func newEndTraceEvent(serviceID akid.ServiceID, traceID akid.LearnSessionID) endTraceEvent {
	return endTraceEvent{
		serviceID: serviceID,
		traceID:   traceID,
	}
}

func (event endTraceEvent) handle(client *cloudClient) {
	serviceInfo, traceInfo := client.getInfo(event.serviceID, event.traceID)
	if serviceInfo == nil {
		printer.Debugf("Tried to end logging for unknown service %q\n", akid.String(event.serviceID))
		return
	}
	if traceInfo == nil {
		printer.Debugf("Tried to end logging for unknown trace %q\n", akid.String(event.traceID))
		return
	}

	traceInfo.active = false

	if len(traceInfo.clientNames) > 0 {
		return
	}

	// No clients are registered for the deactivated trace. Unregister the trace
	// and close the event channel.
	client.unregisterTrace(event.serviceID, event.traceID)
}
