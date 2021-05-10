package cloud_client

import (
	"fmt"
	"net/http"

	"github.com/akitasoftware/akita-cli/har_loader"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/akid"
)

// A request for sending trace events to the daemon.
type traceEventRequest struct {
	// The name of the client that made this request.
	clientName string

	// The service with which the client is associated.
	serviceID akid.ServiceID

	// The trace to which events are to be added.
	traceID akid.LearnSessionID

	// The set of trace events received.
	traceEvents []*TraceEvent

	// Indicates whether this is the last trace-event request for the trace.
	noMoreEvents bool

	// The channel on which to send the response to this request.
	responseChannel chan<- TraceEventResponse
}

func NewTraceEventRequest(clientName string, serviceID akid.ServiceID, traceID akid.LearnSessionID, traceEvents []*TraceEvent, noMoreEvents bool, responseChannel chan<- TraceEventResponse) traceEventRequest {
	return traceEventRequest{
		clientName:      clientName,
		serviceID:       serviceID,
		traceID:         traceID,
		traceEvents:     traceEvents,
		noMoreEvents:    noMoreEvents,
		responseChannel: responseChannel,
	}
}

// A response to a TraceEventRequest.
type TraceEventResponse struct {
	HTTPStatus int
	Body       traceEventResponseBody
}

func newTraceEventResponse(httpStatus int, message string, traceEventDetails *TraceEventDetails) TraceEventResponse {
	return TraceEventResponse{
		HTTPStatus: httpStatus,
		Body:       newTraceEventBody(message, traceEventDetails),
	}
}

// The body of a response to a TraceEventRequest.
type traceEventResponseBody struct {
	Message           string             `json:"message,omitempty"`
	TraceEventDetails *TraceEventDetails `json:"trace_event_details,omitempty"`
}

func newTraceEventBody(message string, traceEventDetails *TraceEventDetails) traceEventResponseBody {
	return traceEventResponseBody{
		Message:           message,
		TraceEventDetails: traceEventDetails,
	}
}

type TraceEvent = har_loader.CustomHAREntry

// Provides details on the processing status of trace events.
type TraceEventDetails struct {
	// How many were dropped because the queue was full.
	Drops int `json:"drops"`
}

// This should only be called from within the main goroutine for the cloud
// client.
func (req traceEventRequest) handle(client *cloudClient) {
	printer.Debugf("Handling incoming %d events for trace %q of service %q\n", len(req.traceEvents), akid.String(req.traceID), akid.String(req.serviceID))
	if req.noMoreEvents {
		printer.Debugf("  Client has signalled no more events\n")
	}

	// See if the service and trace are known by the daemon. If the daemon
	// doesn't know about the service or the trace yet, either the client is
	// misbehaving or the daemon has restarted and lost its state. Either way,
	// the daemon can't accept events until it learns about the trace's logging
	// options from the cloud.
	//
	// If the daemon has been restarted, things will normalize when the first
	// round of polling occurs, and the client can then resume sending trace
	// events. Until that happens, just reject the trace events.
	serviceInfo, traceInfo := client.getInfo(req.serviceID, req.traceID)
	if serviceInfo == nil || traceInfo == nil {
		defer close(req.responseChannel)
		var message string
		if serviceInfo == nil {
			message = fmt.Sprintf("Service %q was not previously registered with this daemon", akid.String(req.serviceID))
		} else {
			message = fmt.Sprintf("Trace %q was not previously registered with this daemon", akid.String(req.traceID))
		}

		req.responseChannel <- newTraceEventResponse(http.StatusBadRequest, message, nil)
		return
	}

	// Register the client with the trace.
	traceInfo.clientNames[req.clientName] = struct{}{}

	// Start a goroutine for relaying the incoming trace events to the collector.
	go uploadTraceEvents(client, req, traceInfo.traceEventChannel)
}

// Sends trace events to the cloud.
func uploadTraceEvents(client *cloudClient, req traceEventRequest, traceEventChannel chan<- *TraceEvent) {
	numTraceEventsDropped := 0

	var err error = nil
	for _, traceEvent := range req.traceEvents {
		// Attempt to enqueue the trace event.
		select {
		case traceEventChannel <- traceEvent:
		default:
			numTraceEventsDropped++
		}
	}

	// Log any errors that we encountered while processing the trace events.
	eventDetails := TraceEventDetails{
		Drops: numTraceEventsDropped,
	}

	// Send the result to the client.
	{
		defer close(req.responseChannel)

		status := http.StatusAccepted
		message := ""

		if err != nil {
			// We encountered malformed trace events. Return a "Bad Request"
			// status.
			status = http.StatusBadRequest
			message = "Malformed trace event encountered"
		} else if numTraceEventsDropped > 0 {
			status = http.StatusUnprocessableEntity
			message = "Not all trace events were processed"
		}

		req.responseChannel <- newTraceEventResponse(status, message, &eventDetails)
	}

	// Unregister the client if it's signalled the end of the event stream.
	if req.noMoreEvents {
		client.eventChannel <- newUnregisterClientFromTrace(req.clientName, req.serviceID, req.traceID)
	}
}

type unregisterClientFromTrace struct {
	clientName string
	serviceID  akid.ServiceID
	traceID    akid.LearnSessionID
}

func newUnregisterClientFromTrace(clientName string, serviceID akid.ServiceID, traceID akid.LearnSessionID) unregisterClientFromTrace {
	return unregisterClientFromTrace{
		clientName: clientName,
		serviceID:  serviceID,
		traceID:    traceID,
	}
}

// This should only be called from within the main goroutine for the cloud
// client.
func (event unregisterClientFromTrace) handle(client *cloudClient) {
	printer.Debugf("Unregistering client %s from trace %s\n", event.clientName, akid.String(event.traceID))
	serviceInfo, traceInfo := client.getInfo(event.serviceID, event.traceID)
	if serviceInfo == nil {
		printer.Debugf("Attempted to unregister client %s from unknown service %q\n", event.clientName, akid.String(event.serviceID))
		return
	}

	if traceInfo == nil {
		printer.Debugf("Attempted to unregister client %s from unknown trace %q\n", event.clientName, akid.String(event.traceID))
		return
	}

	delete(traceInfo.clientNames, event.clientName)
	if traceInfo.active || len(traceInfo.clientNames) > 0 {
		return
	}

	// The trace has been deactivated and the last client has been unregistered
	// from the trace. Unregister the trace itself and close the trace event
	// channel.
	client.unregisterTrace(event.serviceID, event.traceID)
}
