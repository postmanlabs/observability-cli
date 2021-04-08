package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/har_loader"
	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/sampled_err"
)

const WITNESS_BUFFER_SIZE = 10_000

type Witness = har_loader.CustomHAREntry

type Args struct {
	// Required args.
	ClientID akid.ClientID
	Domain   string

	// Optional args.
	PortNumber uint16

	Plugins []plugin.AkitaPlugin
}

var cmdArgs Args

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
	router := mux.NewRouter().StrictSlash(true)

	// Register an endpoint for creating a new learning session.
	router.Handle("/v1/services/{serviceName}/learn/sessions", httpHandler(createLearnSession)).Methods("POST")

	// Register an endpoint for adding witnesses to a learning session. The request body is expected to be a stream of HAR entry objects to be added.
	router.Handle("/v1/services/{serviceName}/learn/sessions/{learnSessionName}/witnesses", httpHandler(addWitnesses)).Methods("POST")

	// Register an endpoint for creating an API model out of a set of learning sessions. The request body is expected to be a JSON object specifying the names of the learning sessions.
	router.Handle("/v1/services/{serviceName}/api-models", httpHandler(createModel)).Methods("POST")

	listenSocket := fmt.Sprintf("127.0.0.1:%d", cmdArgs.PortNumber)
	log.Fatal(http.ListenAndServe(listenSocket, router))
	return nil
}

// Obtains the service ID for the service name contained in the given HTTP request variables. If an error occurs, this is formatted and returned as an HTTP response.
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

// Creates a new learning session in the Akita back end.
func createLearnSession(request *http.Request) HTTPResponse {
	vars := mux.Vars(request)

	// Get the service ID.
	serviceID, httpErr := getServiceID(vars)
	if httpErr != nil {
		return *httpErr
	}

	// Create a new learning session with a random name.
	// TODO: Allow user to specify a name?
	learnSessionName := util.RandomLearnSessionName()
	tags := map[string]string{}
	_, err := util.NewLearnSession(cmdArgs.Domain, cmdArgs.ClientID, serviceID, learnSessionName, tags, nil)
	if err != nil {
		return NewHTTPError(err, http.StatusInternalServerError, "Unable to start learning session")
	}

	// Return an HTTPResponse with the name of the new learning session.
	// XXX Return session ID instead? Return both name and ID? Will either cause issues when used as part of URI?
	return NewHTTPResponse(http.StatusOK,
		struct {
			LearnSessionName string `json:"learnSessionName"`
		}{
			LearnSessionName: learnSessionName,
		})
}

// Adds a set of witnesses to a learning session in the Akita back end.
//
// The request payload is expected to contain a sequence of HAR entries in JSON-serialized format. This entry is added as a witness to the learning session identified in the request.
func addWitnesses(request *http.Request) HTTPResponse {
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

	// Get the learning session ID.
	learnSessionName := vars["learnSessionName"]
	learnClient := rest.NewLearnClient(cmdArgs.Domain, cmdArgs.ClientID, serviceID)
	learnSessionID, err := util.GetLearnSessionIDByName(learnClient, learnSessionName)
	if err != nil {
		return NewHTTPError(err, http.StatusNotFound, "Learning session not found")
	}

	successfulEntries := 0
	sampledErrs := sampled_err.Errors{SampleCount: 3}

	// Start a consumer goroutine that will read witnesses from a channel and pass them on to a backend collector.
	witnessChannel := make(chan *Witness, WITNESS_BUFFER_SIZE)
	doneChannel := make(chan bool)
	go func() {
		defer func() { doneChannel <- true }()

		// Create collector for ingesting the witnesses.
		collector := trace.NewBackendCollector(serviceID, learnSessionID, learnClient, kgxapi.Inbound, cmdArgs.Plugins)

		// TODO: Offer option to remove third-party trackers?

		// Create a new stream ID for the witnesses we are about to process.
		streamID := uuid.New()

		for seqNum := 0; true; seqNum++ {
			// Get the next witness.
			witness, more := <-witnessChannel
			if !more {
				return
			}

			// Pass the witness to the collector.
			entrySuccess := apispec.ProcessHAREntry(collector, streamID, seqNum, *witness, &sampledErrs)
			if entrySuccess {
				successfulEntries += 1
			}
		}
	}()

	// Process the witnesses in the request body.
	numWitnessesParsed := 0
	numWitnessesDropped := 0
	jsonDecoder := json.NewDecoder(request.Body)
	for seqNum := 0; true; seqNum++ {
		// Deserialize the next witness.
		// XXX How to prevent client from giving us extremely large witnesses?
		var witness Witness
		if err := jsonDecoder.Decode(&witness); err != nil {
			if err == io.EOF {
				// We've reached the end of the stream, which isn't really an error.
				err = nil
			}
			break
		}

		numWitnessesParsed++

		// Attempt to enqueue the witness.
		select {
		case witnessChannel <- &witness:
		default:
			numWitnessesDropped++
		}
	}

	// Signal the end of the witness stream and wait for the witnesses to finish uploading.
	close(witnessChannel)
	<-doneChannel

	// Log any errors that we encountered while processing the HAR entries.
	if sampledErrs.TotalCount > 0 {
		log.Printf("Encountered errors with %d HAR entries.\n", numWitnessesParsed-numWitnessesDropped-successfulEntries)
		log.Printf("Sample errors:\n")
		for _, e := range sampledErrs.Samples {
			log.Printf("\t- %s\n", e)
		}
	}

	type witnessDetails struct {
		// How many were parsed as valid witnesses.
		Parsed int `json:"parsed"`

		// How many were dropped because the queue was full.
		Drops int `json:"drops"`

		// How many errors were encountered.
		Errors int `json:"errors"`

		// How many were processed successfully.
		Successes int `json:"successes"`
	}

	type responseBody struct {
		Message        string         `json:"message,omitempty"`
		WitnessDetails witnessDetails `json:"witnessDetails,omitempty"`
	}

	witDetails := witnessDetails{
		Parsed:    numWitnessesParsed,
		Drops:     numWitnessesDropped,
		Errors:    sampledErrs.TotalCount,
		Successes: successfulEntries,
	}

	// If we encountered malformed witnesses or there were errors processing witnesses, return a "Bad Request" status.
	if err != nil || sampledErrs.TotalCount > 0 {
		message := "Errors encountered while processing witnesses"
		if err != nil {
			message = "Malformed witness encountered"
			witDetails.Errors++
		}
		return NewHTTPResponse(http.StatusBadRequest,
			responseBody{
				Message:        message,
				WitnessDetails: witDetails,
			})
	}

	// If we dropped witnesses, return a "Unprocessable Entity" status.
	if numWitnessesDropped > 0 {
		return NewHTTPResponse(http.StatusUnprocessableEntity,
			responseBody{
				Message:        "Not all witnesses were processed",
				WitnessDetails: witDetails,
			})
	}

	// Otherwise, everything went well, so return an "OK" status.
	return NewHTTPResponse(http.StatusOK,
		responseBody{
			WitnessDetails: witDetails,
		})
}

// Creates an API model from a learning session in the Akita back end.
func createModel(request *http.Request) HTTPResponse {
	vars := mux.Vars(request)

	// Ensure the request body is JSON-encoded.
	if httpErr := EnsureJSONEncodedRequestBody(request); httpErr != nil {
		return *httpErr
	}

	// Get the service ID and instantiate a learning client.
	serviceID, httpErr := getServiceID(vars)
	if httpErr != nil {
		return *httpErr
	}
	learnClient := rest.NewLearnClient(cmdArgs.Domain, cmdArgs.ClientID, serviceID)

	// Parse the request body. Expect a JSON serialization of the following type.
	var requestBody struct {
		LearnSessionNames []string `json:"learnSessions"`
	}
	jsonDecoder := json.NewDecoder(request.Body)
	if err := jsonDecoder.Decode(&requestBody); err != nil {
		return NewHTTPError(err, http.StatusBadRequest, "Malformed request body")
	}

	// Convert the learning session names into IDs.
	learnSessionIDs := make([]akid.LearnSessionID, len(requestBody.LearnSessionNames))
	for i, learnSessionName := range requestBody.LearnSessionNames {
		var err error
		learnSessionIDs[i], err = util.GetLearnSessionIDByName(learnClient, learnSessionName)
		if err != nil {
			return NewHTTPError(err, http.StatusNotFound, "Learning session not found: "+learnSessionName)
		}
	}

	// Create a new API model with a random name.
	// TODO: Allow user to specify a name?
	modelName := util.RandomAPIModelName()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outModelID, err := learnClient.CreateSpec(ctx, modelName, learnSessionIDs, rest.CreateSpecOptions{})
	if err != nil {
		return NewHTTPError(err, http.StatusInternalServerError, "Failed to create new spec")
	}

	modelURL := apispec.GetSpecURL(cmdArgs.Domain, serviceID, outModelID)

	return NewHTTPResponse(http.StatusAccepted,
		struct {
			ModelID  string `json:"modelID"`
			ModelURL string `json:"modelURL"`
		}{
			ModelID:  akid.String(outModelID),
			ModelURL: modelURL.String(),
		})
}
