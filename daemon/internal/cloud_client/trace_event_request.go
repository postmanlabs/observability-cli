package cloud_client

import (
	"encoding/json"
	"fmt"
	"io"
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

	// The input stream on which to receive trace events.
	traceEvents *json.Decoder

	// The channel on which to send the response to this request.
	responseChannel chan<- TraceEventResponse
}

func NewTraceEventRequest(clientName string, serviceID akid.ServiceID, traceID akid.LearnSessionID, traceEvents *json.Decoder, responseChannel chan<- TraceEventResponse) traceEventRequest {
	return traceEventRequest{
		clientName:      clientName,
		serviceID:       serviceID,
		traceID:         traceID,
		traceEvents:     traceEvents,
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
	// How many were parsed as valid trace events. Currently, we assume that a
	// JSON object is a valid trace event if it has either a "request" or a
	// "response" field.
	Parsed int `json:"parsed"`

	// How many were dropped because the queue was full.
	Drops int `json:"drops"`
}

// This should only be called from within the main goroutine for the cloud
// client.
func (req traceEventRequest) handle(client *cloudClient) {
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
	traceEventChannel := traceInfo.traceEventChannel
	go func() {
		noMoreEvents := false
		numTraceEventsParsed := 0
		numTraceEventsDropped := 0

		var err error = nil
		for {
			// Deserialize the next trace event.
			var traceEvent TraceEvent
			if err = req.traceEvents.Decode(&traceEvent); err != nil {
				if err == io.EOF {
					// We've reached the end of the stream, which isn't really an error.
					// However, until we receive an empty JSON object from the client, we
					// expect to receive more trace events from the client in a
					// subsequent connection.
					err = nil
				}
				break
			}

			// If the deserialized object has neither a request nor a response, treat
			// it as signalling the end of the trace from this client.
			if traceEvent.Request == nil && traceEvent.Response == nil {
				noMoreEvents = true
				break
			}

			numTraceEventsParsed++

			// Attempt to enqueue the trace event.
			select {
			case traceEventChannel <- &traceEvent:
			default:
				numTraceEventsDropped++
			}
		}

		// Log any errors that we encountered while processing the trace events.
		eventDetails := TraceEventDetails{
			Parsed: numTraceEventsParsed,
			Drops:  numTraceEventsDropped,
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
		if noMoreEvents {
			client.eventChannel <- newUnregisterClientFromTrace(req.clientName, req.serviceID, req.traceID)
		}
	}()
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
	serviceInfo, traceInfo := client.getInfo(event.serviceID, event.traceID)
	if serviceInfo == nil {
		printer.Debugf("Attempted to unregister client from unknown service %q\n", akid.String(event.serviceID))
		return
	}

	if traceInfo == nil {
		printer.Debugf("Attempted to unregister client from unknown trace %q\n", akid.String(event.traceID))
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
