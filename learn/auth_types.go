package learn

import (
	"strings"

	pb "github.com/akitasoftware/akita-ir/go/api_spec"
	"github.com/akitasoftware/akita-libs/spec_util"
)

// Table of prefixes identifying each authorization header type.
// TODO: allow user addition to this list
type authTypePrefix struct {
	Prefix string
	Type   pb.HTTPAuth_HTTPAuthType
}

var knownAuthTypes []authTypePrefix = []authTypePrefix{
	// IANA assigned names
	{"bearer ", pb.HTTPAuth_BEARER},
	{"basic ", pb.HTTPAuth_BASIC},
	{"digest ", pb.HTTPAuth_DIGEST},
	{"mutual ", pb.HTTPAuth_MUTUAL},
	{"oauth ", pb.HTTPAuth_OAUTH},
	{"vapid ", pb.HTTPAuth_VAPID},
	{"scram-sha-1 ", pb.HTTPAuth_SCRAM_SHA_1},
	{"scram-sha-256 ", pb.HTTPAuth_SCRAM_SHA_256},
	{"negotiate ", pb.HTTPAuth_NEGOTIATE},
	{"hoba ", pb.HTTPAuth_HOBA},

	// non-IANA but well-known auth types
	{"aws4-hmac-sha256 ", pb.HTTPAuth_AWS4_HMAC_SHA256},
	{"ntlm ", pb.HTTPAuth_NTLM},
}

// Search the known types list for a matching prefix in
// a case-insensitive fashion.
func findKnownTypeByPrefix(headerValue string) (pb.HTTPAuth_HTTPAuthType, string, bool) {
	lv := strings.ToLower(headerValue)

	for _, t := range knownAuthTypes {
		if strings.HasPrefix(lv, t.Prefix) {
			return t.Type, headerValue[len(t.Prefix):], true
		}
	}
	return pb.HTTPAuth_UNKNOWN, "", false
}

// Parse the "authorization" header and extract its type by prefix
func AuthorizationHeaderType(headerValue string, responseCode int) *pb.Data {
	authType, token, ok := findKnownTypeByPrefix(headerValue)

	if !ok {
		authType = pb.HTTPAuth_UNKNOWN
		// TODO: maybe use the first word as the type, and the
		// remainder as the value?
		token = headerValue
	}

	return newAuthData(authType, token, responseCode)
}

func newAuthData(authType pb.HTTPAuth_HTTPAuthType, token string, responseCode int) *pb.Data {
	return &pb.Data{
		Value: &pb.Data_Primitive{spec_util.CategorizeString(token).Obfuscate().ToProto()},
		Meta: &pb.DataMeta{
			Meta: &pb.DataMeta_Http{
				Http: &pb.HTTPMeta{
					Location:     &pb.HTTPMeta_Auth{Auth: &pb.HTTPAuth{Type: authType}},
					ResponseCode: int32(responseCode),
				},
			},
		},
	}
}

func newAuthDataProprietary(header string, token string, responseCode int) *pb.Data {
	return &pb.Data{
		Value: &pb.Data_Primitive{spec_util.CategorizeString(token).Obfuscate().ToProto()},
		Meta: &pb.DataMeta{
			Meta: &pb.DataMeta_Http{
				Http: &pb.HTTPMeta{
					Location: &pb.HTTPMeta_Auth{Auth: &pb.HTTPAuth{
						Type:              pb.HTTPAuth_PROPRIETARY_HEADER,
						ProprietaryHeader: header,
					}},
					ResponseCode: int32(responseCode),
				},
			},
		},
	}
}

// Table of headers to treat as authorization types. We list a standard form of
// each header, but HTTP headers are not case sensitive.
// TODO: allow user additions to this list.
// TODO: recognize WWW-Authenticate, Optional-WWW-Authenticate, Authentication-Control in responses?
// TODO: recognize and omit Proxy-Authorization?
type authHeader struct {
	// Preferred value
	Header string
}

var knownAuthHeaders []authHeader = []authHeader{
	{"X-Hub-Signature-256"},
	{"X-Hub-Signature"},
}

// Build a lower-cased index
var authHeadersIndex map[string]authHeader = map[string]authHeader{}

func init() {
	for _, h := range knownAuthHeaders {
		authHeadersIndex[strings.ToLower(h.Header)] = h
	}
}

// Check whether the header is one of the known non-standard authorization
// headers.
func NonstandardAuthorizationHeader(headerKey string, headerValue string, responseCode int) (*pb.Data, bool) {
	lv := strings.ToLower(headerKey)
	if auth, ok := authHeadersIndex[lv]; ok {
		return newAuthDataProprietary(auth.Header, headerValue, responseCode), true
	}
	return nil, false
}
