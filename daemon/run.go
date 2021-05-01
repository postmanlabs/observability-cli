package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/akitasoftware/akita-cli/daemon/internal/cloud_client"
	"github.com/akitasoftware/akita-cli/har_loader"
	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/daemon"
)

const TRACE_BUFFER_SIZE = 10_000

type TraceEvent = har_loader.CustomHAREntry

type Args struct {
	// Required args.
	ClientID   akid.ClientID
	Domain     string
	DaemonName string

	// Optional args.
	PortNumber uint16

	Plugins []plugin.AkitaPlugin
}

var cmdArgs Args
var eventChannel chan<- cloud_client.Event

// Produces an HTTPResponse from an *http.Request.
type httpRequestHandler func(*http.Request) HTTPResponse

// A wrapper for converting httpRequestHandlers into http.Handlers.
func httpHandler(requestHandler httpRequestHandler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response := requestHandler(request)
		response.Write(writer)
	})
}

func Run(args Args) error {
	cmdArgs = args
	eventChannel = cloud_client.Run(args.DaemonName, args.Domain, args.ClientID, args.Plugins)

	router := mux.NewRouter().StrictSlash(true)

	// Endpoint registration
	{
		// Used by middleware to long-poll for changes in the set of activated
		// traces for a service.
		router.Handle("/v1/services/{serviceName}/middleware", httpHandler(handleMiddlewareRegistration)).Methods("POST")

		// Adds events to a trace. The request body is expected to be a stream of
		// HAR entry objects to be added. Optionally, the body can be terminated
		// with a termination object. When this happens, this signals that the
		// client has no more events to send for the trace.
		router.Handle("/v1/services/{serviceName}/traces/{traceName}/events", httpHandler(addEvents)).Methods("POST")
	}

	listenSocket := fmt.Sprintf("127.0.0.1:%d", cmdArgs.PortNumber)
	log.Fatal(http.ListenAndServe(listenSocket, router))
	return nil
}

// Obtains the service ID for the service name contained in the given HTTP
// request variables. If an error occurs, this is formatted and returned as an
// HTTP response.
func getServiceID(requestVars map[string]string) (akid.ServiceID, *HTTPResponse) {
	serviceName := requestVars["serviceName"]
	frontClient := rest.NewFrontClient(cmdArgs.Domain, cmdArgs.ClientID)
	result, err := util.GetServiceIDByName(frontClient, serviceName)
	if err != nil {
		httpErr := NewHTTPError(err, http.StatusNotFound, "Service not found")
		return result, &httpErr
	}
	return result, nil
}

// Obtains the service ID and trace ID for the service name and trace name
// contained in the given HTTP request variables. If an error occurs, this is
// formatted and returned as an HTTP response.
func getTraceID(requestVars map[string]string) (akid.ServiceID, akid.LearnSessionID, *HTTPResponse) {
	serviceID, httpErr := getServiceID(requestVars)
	if httpErr != nil {
		return serviceID, akid.LearnSessionID{}, httpErr
	}

	learnClient := rest.NewLearnClient(cmdArgs.Domain, cmdArgs.ClientID, serviceID)
	traceName := requestVars["traceName"]
	traceID, err := util.GetLearnSessionIDByName(learnClient, traceName)
	if err != nil {
		httpErr := NewHTTPError(err, http.StatusNotFound, "Trace not found")
		return serviceID, traceID, &httpErr
	}
	return serviceID, traceID, nil
}

// Waits for the set of active traces to change for a service and sends
// a diff as a response to the request.
func handleMiddlewareRegistration(request *http.Request) HTTPResponse {
	vars := mux.Vars(request)

	// Ensure the request body is JSON-encoded.
	if httpErr := EnsureJSONEncodedRequestBody(request); httpErr != nil {
		return *httpErr
	}
	jsonDecoder := json.NewDecoder(request.Body)

	// Get the service ID.
	serviceID, httpErr := getServiceID(vars)
	if httpErr != nil {
		return *httpErr
	}

	// Parse the request body.
	var requestBody struct {
		clientName string

		// The IDs of the traces for which the client is currently logging.
		ActiveTraceIDs []akid.LearnSessionID `json:"active_trace_ids"`
	}
	if err := jsonDecoder.Decode(&requestBody); err != nil {
		return NewHTTPError(err, http.StatusBadRequest, "Invalid request body")
	}

	// Wait for the set of active traces to change from what the client has sent.
	responseChannel := make(chan daemon.ActiveTraceDiff)
	eventChannel <- cloud_client.NewRegistrationRequest(requestBody.clientName, serviceID, requestBody.ActiveTraceIDs, responseChannel)
	newTraces := <-responseChannel

	return NewHTTPResponse(http.StatusAccepted, newTraces)
}

// Adds a set of events to a trace in the Akita back end.
//
// The request payload is expected to contain a sequence of HAR entries in
// JSON-serialized format. These entries are added as events to the trace
// identified in the request URL.
func addEvents(request *http.Request) HTTPResponse {
	vars := mux.Vars(request)

	// Ensure the request body is JSON-encoded.
	if httpErr := EnsureJSONEncodedRequestBody(request); httpErr != nil {
		return *httpErr
	}

	// Get the service ID.
	serviceID, httpErr := getServiceID(vars)
	if httpErr != nil {
		return *httpErr
	}

	// Get the trace ID.
	traceName := vars["traceName"]
	learnClient := rest.NewLearnClient(cmdArgs.Domain, cmdArgs.ClientID, serviceID)
	traceID, err := util.GetLearnSessionIDByName(learnClient, traceName)
	if err != nil {
		return NewHTTPError(err, http.StatusNotFound, "Trace not found")
	}

	// Get the request header.
	jsonDecoder := json.NewDecoder(request.Body)
	var requestHeader struct {
		ClientName string `json:"client_name"`
	}
	if err := jsonDecoder.Decode(&requestHeader); err != nil {
		return NewHTTPError(err, http.StatusBadRequest, "Bad request body")
	}

	// Hand the request off to the cloud client.
	responseChannel := make(chan cloud_client.TraceEventResponse)
	eventChannel <- cloud_client.NewTraceEventRequest(
		requestHeader.ClientName,
		serviceID,
		traceID,
		jsonDecoder,
		responseChannel)
	response := <-responseChannel

	return NewHTTPResponse(response.HTTPStatus, response.Body)
}
