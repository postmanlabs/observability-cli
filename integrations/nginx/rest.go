package nginx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"

	"github.com/akitasoftware/akita-cli/daemon"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/gorilla/mux"
)

var separator string = "\n-----------------------------------------------"

// Print a request payload to the console
func dumpRequest(rw http.ResponseWriter, req *http.Request) {
	// Check for JSON encoding
	if httpErr := daemon.EnsureJSONEncodedRequestBody(req); httpErr != nil {
		fmt.Println("Expected application/json content-type")
		dumpGeneric(rw, req)
		return
	}

	jsonDecoder := json.NewDecoder(req.Body)
	var m MirroredRequest
	if err := jsonDecoder.Decode(&m); err != nil {
		fmt.Println("Error during decode: ", err)
		dumpGeneric(rw, req)
		return
	}

	fmt.Println("Request", m.RequestID, m.Method, m.Host, m.Path)
	fmt.Println("Timestamps:", m.RequestStart, "--", m.RequestArrived)
	fmt.Println("Headers:")
	for _, h := range m.Headers {
		fmt.Printf("  %s: %s\n", h.Header, h.Value)
	}
	if truncSize, trunc := m.Truncated.Get(); trunc {
		fmt.Printf("Body, truncated to %d bytes:\n", truncSize)
	} else {
		fmt.Println("Body:")
	}
	fmt.Println(m.Body)
	fmt.Println(separator)

	rw.WriteHeader(200)
	rw.Header().Set("Content-type", "text/plain")
	rw.Write([]byte("OK"))
}

// Print a response payload to the console
func dumpResponse(rw http.ResponseWriter, req *http.Request) {
	// Check for JSON encoding
	if httpErr := daemon.EnsureJSONEncodedRequestBody(req); httpErr != nil {
		fmt.Println("Expected application/json content-type")
		dumpGeneric(rw, req)
		return
	}

	jsonDecoder := json.NewDecoder(req.Body)
	var m MirroredResponse
	if err := jsonDecoder.Decode(&m); err != nil {
		fmt.Println("Error during decode: ", err)
		dumpGeneric(rw, req)
		return
	}

	fmt.Println("Response", m.RequestID)
	fmt.Println("Timestamps:", m.ResponseStart, m.ResponseComplete)
	fmt.Println("Response code:", m.ResponseCode)
	fmt.Println("Headers:")
	for _, h := range m.Headers {
		fmt.Printf("  %s: %s\n", h.Header, h.Value)
	}
	if truncSize, trunc := m.Truncated.Get(); trunc {
		fmt.Printf("Body, truncated to %d bytes:\n", truncSize)
	} else {
		fmt.Println("Body:")
	}
	fmt.Println(m.Body)
	fmt.Println(separator)

	rw.WriteHeader(200)
	rw.Header().Set("Content-type", "text/plain")
	rw.Write([]byte("OK"))
}

// Print a generic request to the console
func dumpGeneric(rw http.ResponseWriter, req *http.Request) {
	fmt.Print("Unexpected path")
	b, err := httputil.DumpRequest(req, true)
	if err != nil {
		fmt.Println("Error: ", err)
	} else {
		os.Stdout.Write(b)
	}
	fmt.Println(separator)

	rw.WriteHeader(200)
	rw.Header().Set("Content-type", "text/plain")
	rw.Write([]byte("OK"))
}

// Open a web server that dumps the output to the console and returns 200
// for everything. Used for NGINX module development as a sink for its HTTP calls.
// TODO: a way to inject errors
func RunDevelopmentServer(listenPort uint16) error {
	printer.Infof("Listening on port %d in development mode...\n", listenPort)

	r := mux.NewRouter()
	r.HandleFunc("/trace/v1/request", dumpRequest).Methods("POST")
	r.HandleFunc("/trace/v1/response", dumpResponse).Methods("POST")
	r.PathPrefix("/").HandlerFunc(dumpGeneric)

	listenAddress := fmt.Sprintf(":%d", listenPort)
	return http.ListenAndServe(listenAddress, r)
}

func RunServer(listenPort uint16) error {
	return fmt.Errorf("This command is not yet implemented.")
}
