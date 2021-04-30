package daemon

// Some of this is loosely based on
// https://medium.com/@ozdemir.zynl/rest-api-error-handling-in-go-behavioral-type-assertion-509d93636afd

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/golang/gddo/httputil/header"
)

// Ensures the request body is JSON-encoded. If it is not, returns an
// HTTPResponse indicating an error. Otherwise, returns nil.
func EnsureJSONEncodedRequestBody(request *http.Request) *HTTPResponse {
	contentType := ""
	if request.Header.Get("Content-Type") != "" {
		contentType, _ = header.ParseValueAndParams(request.Header, "Content-Type")
	}
	if contentType != "application/json" {
		httpErr := NewHTTPError(nil, http.StatusUnsupportedMediaType, "Content-Type header is not \"application/json\"")
		return &httpErr
	}
	return nil
}

// Use rest.HTTPError as an HTTP response. Even though its name suggests that it
// represents an error, HTTPError has all of the elements needed to encapsulate
// a response.
type HTTPResponse rest.HTTPError

// Obtains the JSON body of an HTTP response.
func (response *HTTPResponse) ResponseBody() []byte {
	return response.Body
}

// Produces the response code and a set of headers for an HTTP response.
func (response *HTTPResponse) ResponseHeaders() (int, map[string]string) {
	return response.StatusCode, map[string]string{
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

// HTTPResponse constructor. If the given body cannot be serialized into JSON,
// this produces a status-500 response with an empty body, and an error is
// logged.
func NewHTTPResponse(status int, body interface{}) HTTPResponse {
	var bodyJson []byte = nil
	var err error = nil

	if body != nil {
		if bodyJson, err = json.Marshal(body); err != nil {
			printer.Errorf("An error occurred while serializing an HTTPResponse body: %v\n", err)
			return NewHTTPResponse(http.StatusInternalServerError, nil)
		}
	}
	return HTTPResponse{
		StatusCode: status,
		Body:       bodyJson,
	}
}

// Convenience method for creating HTTPResponses that represent errors. If the
// given error is a rest.HTTPError, then this is used as is, and the remaining
// arguments are ignored.
func NewHTTPError(err error, status int, message string) HTTPResponse {
	var httpErr rest.HTTPError
	if errors.As(err, &httpErr) {
		// Just use the HTTPError as is.
		return HTTPResponse(httpErr)
	}

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
