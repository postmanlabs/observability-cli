package cloud_client

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/akitasoftware/akita-cli/har_loader"
	"github.com/akitasoftware/akita-libs/akid"
)

// A request for sending trace events to the daemon.
type TraceEventRequest struct {
	// The service with which the client is associated.
	ServiceID akid.ServiceID

	// The trace to which events are to be added.
	TraceID akid.LearnSessionID

	// The input stream on which to receive trace events.
	TraceEvents io.ReadCloser

	// The channel on which to send the response to this request.
	ResponseChannel chan<- TraceEventResponse
}

// A response to a TraceEventRequest.
type TraceEventResponse struct {
	HTTPStatus int
	Body       TraceEventResponseBody
}

// The body of a response to a TraceEventRequest.
type TraceEventResponseBody struct {
	Message           string             `json:"message,omitempty"`
	TraceEventDetails *TraceEventDetails `json:"traceEventDetails,omitempty"`
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

func (req TraceEventRequest) handle(client *cloudClient) {
	// See if the service is one that is known by the daemon. If the daemon
	// doesn't know about the service yet, either the client is misbehaving
	// client or the daemon has restarted and lost its state. Either way, the
	// daemon can't accept events until it learns about the trace's logging
	// options from the cloud.
	//
	// If the daemon has been restarted, a proper client will retry its long-poll
	// for logging-status updates. Things will normalize when that happens, and
	// the client can then resume sending trace events. Until that happens, just
	// reject the trace events.
	if _, ok := client.serviceInfoByID[req.ServiceID]; !ok {
		defer close(req.ResponseChannel)
		req.ResponseChannel <- TraceEventResponse{
			HTTPStatus: http.StatusBadRequest,
			Body: TraceEventResponseBody{
				Message: fmt.Sprintf("Service %q was not previously registered with this daemon", req.ServiceID),
			},
		}
		return
	}

	// Ensure we have a collector for the trace.
	traceInfo := client.ensureTraceEventCollector(req.ServiceID, req.TraceID)
	if traceInfo == nil {
		// Either the client is misbehaving or the daemon is still recovering its
		// state from a restart. See above comment.
		defer close(req.ResponseChannel)
		req.ResponseChannel <- TraceEventResponse{
			HTTPStatus: http.StatusBadRequest,
			Body: TraceEventResponseBody{
				Message: fmt.Sprintf("Logging has not started on this daemon for service %q", req.ServiceID),
			},
		}
		return
	}

	// Register the client with the trace.
	traceInfo.numClients++

	// Start a goroutine for relaying the incoming trace events to the collector.
	traceEventChannel := traceInfo.traceEventChannel
	go func() {
		jsonDecoder := json.NewDecoder(req.TraceEvents)
		noMoreEvents := false
		numTraceEventsParsed := 0
		numTraceEventsDropped := 0

		var err error = nil
		for {
			// Deserialize the next trace event.
			var traceEvent TraceEvent
			if err = jsonDecoder.Decode(&traceEvent); err != nil {
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
			defer close(req.ResponseChannel)

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

			req.ResponseChannel <- TraceEventResponse{
				HTTPStatus: status,
				Body: TraceEventResponseBody{
					Message:           message,
					TraceEventDetails: &eventDetails,
				},
			}
		}

		// Unregister the client if it's signalled the end of the event stream.
		if noMoreEvents {
			client.eventChannel <- newUnregisterClientFromTrace(req.ServiceID, req.TraceID)
		}
	}()
}

type unregisterClientFromTrace struct {
	serviceID akid.ServiceID
	traceID   akid.LearnSessionID
}

func newUnregisterClientFromTrace(serviceID akid.ServiceID, traceID akid.LearnSessionID) unregisterClientFromTrace {
	return unregisterClientFromTrace{
		serviceID: serviceID,
		traceID:   traceID,
	}
}

func (event unregisterClientFromTrace) handle(client *cloudClient) {
	serviceInfo, ok := client.serviceInfoByID[event.serviceID]
	if !ok {
		log.Printf("Attempted to unregister client from unknown service %q", event.serviceID)
		return
	}

	traceInfo, ok := serviceInfo.Traces[event.traceID]
	if !ok {
		log.Printf("Attempted to unregister client from unknown trace %q", event.traceID)
		return
	}

	traceInfo.numClients--
	if traceInfo.numClients > 0 {
		return
	}

	// The last client has been unregistered from the trace. Unregister the
	// trace itself and close the trace event channel.
	defer close(traceInfo.traceEventChannel)
	delete(serviceInfo.Traces, event.traceID)
}
