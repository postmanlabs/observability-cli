package trace

import (
	"net/http"

	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/spec_util"
)

func containsCLITraffic(t akinet.ParsedNetworkTraffic) bool {
	var header http.Header
	switch tc := t.Content.(type) {
	case akinet.HTTPRequest:
		header = tc.Header
	case akinet.HTTPResponse:
		header = tc.Header
	default:
		return false
	}

	for _, k := range []string{spec_util.XAkitaCLIGitVersion, spec_util.XAkitaRequestID} {
		if header.Get(k) != "" {
			return true
		}
	}
	return false
}
