package daemon

// Some of this is loosely based on https://medium.com/@ozdemir.zynl/rest-api-error-handling-in-go-behavioral-type-assertion-509d93636afd

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/golang/gddo/httputil/header"
)

// Ensures the request body is JSON-encoded. If it is not, returns an HTTPResponse indicating an error. Otherwise, returns nil.
func EnsureJSONEncodedRequestBody(request *http.Request) *HTTPResponse {
	contentType := ""
	if request.Header.Get("Content-Type") != "" {
		contentType, _ = header.ParseValueAndParams(request.Header, "Content-Type")
	}
	if contentType != "application/json" {
		return NewHTTPError(nil, http.StatusUnsupportedMediaType, "Content-Type header is not \"application/json\"")
	}
	return nil
}

// Encapsulates an HTTP status code and a set of headers with a JSON response body.
type ClientResponse interface {
	// Returns the JSON body of the response.
	ResponseBody() []byte

	// Returns the HTTP status code and headers for the response.
	ResponseHeaders() (int, map[string]string)
}

// Implements ClientResponse.
type HTTPResponse struct {
	// The HTTP status code.
	Status int

	// The body of the response, serialized as JSON.
	Body []byte
}

// Obtains the JSON body of an HTTP response.
func (response *HTTPResponse) ResponseBody() []byte {
	return response.Body
}

// Produces the response code and a set of headers for an HTTP response.
func (response *HTTPResponse) ResponseHeaders() (int, map[string]string) {
	return response.Status, map[string]string{
		"Content-Type": "application/json; charset=utf-8",
	}
}

// Writes an HTTP response to the network.
func (response *HTTPResponse) Write(writer http.ResponseWriter) {
	status, headers := response.ResponseHeaders()
	for k, v := range headers {
		writer.Header().Set(k, v)
	}
	writer.WriteHeader(status)
	writer.Write(response.ResponseBody())
}

// HTTPResponse constructor. If the given body cannot be serialized into JSON, this produces a status-500 response with an empty body, and an error is logged.
func NewHTTPResponse(status int, body interface{}) *HTTPResponse {
	var bodyJson []byte = nil
	var err error = nil

	if body != nil {
		if bodyJson, err = json.Marshal(body); err != nil {
			log.Printf("An error occurred while serializing an HTTPResponse body: %v", err)
			return NewHTTPResponse(http.StatusInternalServerError, nil)
		}
	}
	return &HTTPResponse{
		Status: status,
		Body:   bodyJson,
	}
}

// Convenience method for creating HTTPResponses that represent errors.
func NewHTTPError(err error, status int, message string) *HTTPResponse {
	detail := ""
	if err != nil {
		detail = err.Error()
	}

	return NewHTTPResponse(status,
		struct {
			Message string `json:"message,omitempty"`
			Detail  string `json:"detail,omitempty"`
		}{
			Message: message,
			Detail:  detail,
		})
}
