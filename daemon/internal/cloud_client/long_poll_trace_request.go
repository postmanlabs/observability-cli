package cloud_client

import "github.com/akitasoftware/akita-libs/akid"

// A long-poll request from a client waiting to be notified of the deactivation
// of a trace.
type longPollTraceRequest struct {
	serviceID akid.ServiceID
	traceID   akid.LearnSessionID

	// The channel on which the client is waiting for a response.
	responseChannel chan<- struct{}
}

func NewLongPollTraceRequest(serviceID akid.ServiceID, traceID akid.LearnSessionID, responseChannel chan<- struct{}) longPollTraceRequest {
	return longPollTraceRequest{
		serviceID:       serviceID,
		traceID:         traceID,
		responseChannel: responseChannel,
	}
}

func (req longPollTraceRequest) handle(client *cloudClient) {
	// If the service or the trace is not registered with the daemon, tell the
	// client to stop logging.
	serviceInfo, traceInfo := client.getInfo(req.serviceID, req.traceID)
	if serviceInfo == nil || traceInfo == nil {
		req.responseChannel <- struct{}{}
		return
	}

	// If the trace has been deactivated, tell the client to stop logging.
	if !traceInfo.active {
		req.responseChannel <- struct{}{}
		return
	}

	// Add the client to the list waiting to be notified.
	traceInfo.deactivationChannels = append(traceInfo.deactivationChannels, req.responseChannel)
}
