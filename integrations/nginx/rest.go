package nginx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/daemon"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/memview"
)

const (
	// Fake port number used to report nginx captures
	fakeNginxPort = 80

	// Separator for dumping to console
	separator = "\n-----------------------------------------------"
)

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

	writeTextResponse(rw, 200, "OK")
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

	writeTextResponse(rw, 200, "OK")
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

	writeTextResponse(rw, 200, "OK")
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

func writeTextResponse(rw http.ResponseWriter, rc int, text string) {
	rw.WriteHeader(rc)
	rw.Header().Set("Content-type", "text/plain")
	rw.Write([]byte(text))
}

// Convert the schema's MirroredHeader slice to the standard http.Header map
func schemaToHttpHeader(headers []MirroredHeader) http.Header {
	ret := make(http.Header)
	for _, h := range headers {
		ret[h.Header] = append(ret[h.Header], h.Value)
	}
	return ret
}

// Find any Cookies or Set-Cookies headers and create http.Cookies out of them.
// This is what readCookies/readSetCookies in net/http/cookie.go do, but are unexported.
// Instead we create a fake http Request or Response and ask it to do our parsing.
// (Most of which we ignore in the conversion to IR?)
func parseCookiesRequest(h http.Header) []*http.Cookie {
	// TODO: do we need to sanitize the case on these?
	// The code only appears to check for "Set-Cookie" but it could be "set-cookie"?
	r := &http.Request{Header: h}
	return r.Cookies()
}

func parseCookiesResponse(h http.Header) []*http.Cookie {
	// TODO: do we need to sanitize the case on these?
	// The code only appears to check for "Set-Cookie" but it could be "set-cookie"?
	r := &http.Response{Header: h}
	return r.Cookies()
}

func (b *NginxBackend) handleRequest(rw http.ResponseWriter, req *http.Request) {
	// Check for JSON encoding
	if httpErr := daemon.EnsureJSONEncodedRequestBody(req); httpErr != nil {
		telemetry.RateLimitError("NGINX handleRequest", errors.New("Bad content-type"))
		writeTextResponse(rw, 400, "Expecting application/json body")
		return
	}

	jsonDecoder := json.NewDecoder(req.Body)
	var m MirroredRequest
	if err := jsonDecoder.Decode(&m); err != nil {
		printer.Errorf("Could not interpret call from NGINX: %v\n", err)
		telemetry.RateLimitError("NGINX handleRequest", err)
		writeTextResponse(rw, 400, "JSON decode error")
		return
	}

	requestId, err := uuid.Parse(m.RequestID)
	if err != nil {
		printer.Errorf("Could not parse request ID from NGINX: %v\n", err)
		telemetry.RateLimitError("NGINX handleRequest", err)
		writeTextResponse(rw, 400, "Bad request ID")
	}

	headers := schemaToHttpHeader(m.Headers)

	// TODO: check for duplicates due to internal redirection
	pnt := akinet.ParsedNetworkTraffic{
		// Leave IP addresses all zero?
		// In a subsequent revision we could get some of this from Nginx, maybe.  We lack:
		//   * Source address and port
		//   * Destination address and port
		//   * Protocol major/minor version
		//   * Whether HTTP or HTTPS?
		SrcPort:         54321,
		DstPort:         fakeNginxPort,
		Interface:       "nginx",
		ObservationTime: m.RequestStart,
		FinalPacketTime: m.RequestArrived,
		Content: akinet.HTTPRequest{
			StreamID:   requestId,
			Seq:        1, // Every request has its own ID
			Method:     m.Method,
			ProtoMajor: 1,
			ProtoMinor: 0,
			URL: &url.URL{
				Scheme: "http",
				Host:   m.Host,
				Path:   m.Path,
			},
			Host:             m.Host,
			Header:           headers,
			Body:             memview.New([]byte(m.Body)),
			BodyDecompressed: false,
			Cookies:          parseCookiesRequest(headers),
		},
	}
	b.collector.Process(pnt)

	// Log success message to the console
	b.ReportSuccess(m.Host)

	writeTextResponse(rw, 200, "OK")
}

func (b *NginxBackend) handleResponse(rw http.ResponseWriter, req *http.Request) {
	// Check for JSON encoding
	if httpErr := daemon.EnsureJSONEncodedRequestBody(req); httpErr != nil {
		telemetry.RateLimitError("NGINX handleResonse", errors.New("Bad content-type"))
		writeTextResponse(rw, 400, "Expecting application/json body")
		return
	}

	jsonDecoder := json.NewDecoder(req.Body)
	var m MirroredResponse
	if err := jsonDecoder.Decode(&m); err != nil {
		printer.Errorf("Could not interpret call from NGINX: %v\n", err)
		telemetry.RateLimitError("NGINX handleResponse", err)
		writeTextResponse(rw, 400, "JSON decode error")
		return
	}

	requestId, err := uuid.Parse(m.RequestID)
	if err != nil {
		printer.Errorf("Could not parse request ID from NGINX: %v\n", err)
		telemetry.RateLimitError("NGINX handleResponse", err)
		writeTextResponse(rw, 400, "Bad request ID")
	}

	headers := schemaToHttpHeader(m.Headers)

	// TODO: see limitations in request handling
	pnt := akinet.ParsedNetworkTraffic{
		SrcPort:         54321,
		DstPort:         fakeNginxPort,
		Interface:       "nginx",
		ObservationTime: m.ResponseStart,
		FinalPacketTime: m.ResponseComplete,
		Content: akinet.HTTPResponse{
			StreamID:         requestId,
			Seq:              1,
			ProtoMajor:       1,
			ProtoMinor:       0,
			StatusCode:       m.ResponseCode,
			Header:           headers,
			Body:             memview.New([]byte(m.Body)),
			BodyDecompressed: false,
			Cookies:          parseCookiesResponse(headers),
		},
	}
	b.collector.Process(pnt)

	writeTextResponse(rw, 200, "OK")
}

func (b *NginxBackend) handleUnexpected(rw http.ResponseWriter, req *http.Request) {
	printer.Errorf("Unexpected request from NGINX: %v %v\n", req.Method, req.URL)
	telemetry.RateLimitError("NGINX handleUnexpected",
		fmt.Errorf("Request for %v %v", req.Method, req.URL))
	writeTextResponse(rw, 404, "Unexpected path or method")
}

func RunServer(args *Args) error {
	b, err := NewNginxBackend(args)
	if err != nil {
		return nil
	}

	printer.Infof("Listening on port %d for traffic from the NGINX module...\n", args.ListenPort)

	// TODO: apidump does this in TelemetryWorker but that might be a mistake.
	b.SendInitialTelemetry()
	done := make(chan struct{})
	defer close(done)
	go b.TelemetryWorker(done)

	r := mux.NewRouter()
	r.HandleFunc("/trace/v1/request", b.handleRequest).Methods("POST")
	r.HandleFunc("/trace/v1/response", b.handleResponse).Methods("POST")
	r.PathPrefix("/").HandlerFunc(b.handleUnexpected)

	listenAddress := fmt.Sprintf(":%d", args.ListenPort)
	err = http.ListenAndServe(listenAddress, r)
	if err != nil {
		// TODO: Nginx-specific telemetry error types?
		b.SendErrorTelemetry(api_schema.ApidumpError_Other, err)
	}
	return err
}
