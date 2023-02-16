package nginx

import (
	"time"

	"github.com/akitasoftware/go-utils/optionals"
)

/* Rest API schema objects for the communcation between
   the NGINX module and the Akita agent */

// An incoming request to Nginx.
type MirroredRequest struct {
	// Request metadata -- unique ID, time when request processing
	// started and ended, and whether the request is Nginx-internal.
	// Because of internal redirects, the same request_id might appear
	// multiple times. We should probably take just the first (by start time.)
	RequestID      string    `json:"request_id"`
	RequestStart   time.Time `json:"request_start"`
	RequestArrived time.Time `json:"request_arrived"`
	NginxInternal  bool      `json:"nginx_internal"`

	// HTTP header information
	Method  string           `json:"method"`
	Host    string           `json:"host"`
	Path    string           `json:"path"`
	Headers []MirroredHeader `json:"headers"`

	// HTTP body; may be empty. Optionally includes the 'truncated'
	// field specifying the length
	Body      string                  `json:"body"`
	Truncated optionals.Optional[int] `json:"truncated"`
}

// Header name and value
type MirroredHeader struct {
	Header string `json:"header"`
	Value  string `json:"value"`
}

// A response sent by Nginx.
type MirroredResponse struct {
	// Response metadata; has the same unique ID as the request.
	// Times measure when header was first sent and when last byte of body was sent.
	RequestID        string    `json:"request_id"`
	ResponseStart    time.Time `json:"response_start"`
	ResponseComplete time.Time `json:"response_complete"`

	// HTTP header information
	ResponseCode int              `json:"response_code"`
	Headers      []MirroredHeader `json:"headers"`

	// HTTP body; may be empty. Optionally includes the 'truncated'
	// field specifying the length
	Body      string                  `json:"body"`
	Truncated optionals.Optional[int] `json:"truncated"`
}
