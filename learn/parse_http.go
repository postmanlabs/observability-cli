package learn

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/golang/protobuf/proto"

	"github.com/andybalholm/brotli"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"
	"gopkg.in/yaml.v2"

	pb "github.com/akitasoftware/akita-ir/go/api_spec"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/memview"
	"github.com/akitasoftware/akita-libs/spec_util"
	"github.com/akitasoftware/akita-libs/spec_util/ir_hash"
	"github.com/akitasoftware/go-utils/optionals"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
)

var (
	// List of compression algorithms to use as fallback if we can't decode the
	// body and no Content-Encoding header is set.
	// https://app.clubhouse.io/akita-software/story/1656
	fallbackDecompressions = []string{
		"deflate",
		"gzip",
		"br",
	}
)

const (
	// The fallback to trying compression algorithms is more exprensive because there doesn't seem to be a
	// good way of interrogating the algorithms about whether the stream is OK. So we limit the amount of
	// data is may consume or produce.
	MaxFallbackInput  = 1 * 1024 * 1024
	MaxFallbackOutput = 10 * 1024 * 1024

	// This limit is used for non-YAML and non-JSON types that we can have some hope of parsing.
	MaxBufferedBody = 5 * 1024 * 1024

	// For types where we just return a string (or maybe an int) then it doesn't make
	// sense to pull in a lot of data, just to hash it anyway.  The only reason to have more than
	// a few bytes is so we can more reliably distinguish whether responses are identical.
	SmallBodySample = 10 * 1024
)

// These need to be constructors, rather than a global var that's reused, so
// that there is not a race condition when marshaling to protobufs that share
// them. (The race condition actually manifested in obfuscate().)
func UnassignedHTTPID() *pb.MethodID {
	return &pb.MethodID{
		Name:    "",
		ApiType: pb.ApiType_HTTP_REST,
	}
}

func UnknownHTTPMethodMeta() *pb.MethodMeta {
	return &pb.MethodMeta{
		Meta: &pb.MethodMeta_Http{
			Http: &pb.HTTPMethodMeta{
				Method:       "",
				PathTemplate: "",
				Host:         "",
			},
		},
	}
}

type ParseAPISpecError string

func (pase ParseAPISpecError) Error() string {
	return string(pase)
}

func ParseHTTP(elem akinet.ParsedNetworkContent) (*PartialWitness, error) {
	var isRequest bool
	var rawBody memview.MemView
	var bodyDecompressed bool
	var methodMeta *pb.MethodMeta
	var datas []*pb.Data
	var headers http.Header
	statusCode := 0

	var streamID uuid.UUID
	var seq int
	xForwardedFor := optionals.None[string]()

	switch t := elem.(type) {
	case akinet.HTTPRequest:
		streamID = t.StreamID
		seq = t.Seq
		isRequest = true
		methodMeta, datas, xForwardedFor = parseRequest(&t)
		rawBody = t.Body
		bodyDecompressed = t.BodyDecompressed
		headers = t.Header
	case akinet.HTTPResponse:
		streamID = t.StreamID
		seq = t.Seq

		datas = parseResponse(&t)
		rawBody = t.Body
		bodyDecompressed = t.BodyDecompressed
		headers = t.Header
		statusCode = t.StatusCode
		methodMeta = UnknownHTTPMethodMeta()
	default:
		return nil, ParseAPISpecError("expected http message, got something else")
	}

	if rawBody.Len() > 0 {
		bodyStream := rawBody.CreateReader()
		decodeStream, err := decodeBody(headers, bodyStream, bodyDecompressed)
		if err != nil {
			return nil, errors.Wrap(err, "failed to decode body")
		}

		contentType := headers.Get("Content-Type")
		bodyData, err := parseBody(contentType, decodeStream, statusCode)
		if err != nil {
			// TODO: maybe don't do this if we *did* get a Content-Encoding header?
			//
			// Try common decompression algorithms to see if the body is compressed
			// but did not have Content-Encoding header.
			printer.Debugf("Failed to parse body, attempting common decompressions: %v\n", err)
			fallbackReader, decompressErr := attemptDecompress(rawBody)
			if decompressErr == nil {
				bodyData, err = parseBody(contentType, fallbackReader, statusCode)
			}
		}

		if err != nil {
			// Just log an error instead of returning an error so users can see the
			// other parts of the endpoint in the spec rather than an empty spec.
			// https://app.clubhouse.io/akita-software/story/1898/juan-s-payload-problem
			telemetry.RateLimitError("unparsable body", err)
			printer.Debugf("skipping unparsable body: %v\n", err)
		} else if bodyData != nil {
			datas = append(datas, bodyData)
		}
	}

	method := &pb.Method{Id: UnassignedHTTPID(), Meta: methodMeta}

	// Transform our array of datas into a map.
	// We assign sequential string IDs in order to provide a consistent ordering
	dataMap := map[string]*pb.Data{}
	for _, d := range datas {
		// Use the hash of the data proto as the key so we can deterministically
		// compare witnesses.
		//
		// Some witnesses contain duplicate data elements, such as cookies with
		// the same name and value (but different domains or paths, which we
		// don't currently include in the IR).
		//
		// If there is a potential hash collision, fall back to an expensive
		// equality check.  If the witnesses are different, return an error.
		k := ir_hash.HashDataToString(d)
		if existing, collision := dataMap[k]; collision && !proto.Equal(d, existing) {
			return nil, errors.Errorf("detected collision in data map key")
		}
		dataMap[k] = d
	}

	if isRequest {
		method.Args = dataMap
	} else {
		method.Responses = dataMap
	}

	return &PartialWitness{
		Witness:       &pb.Witness{Method: method},
		PairKey:       toWitnessID(streamID, seq),
		XForwardedFor: xForwardedFor,
	}, nil
}

// Today body has been completely assembled, but we accept a Reader to allow us to
// stream throught he decompression later, if it becomes feasible.
//
// TODO: some of the compression algorithms return a ReadCloser, but it
// doesn't look like there's a good standard library way to propogate closes
// all the way back.  So they'd all have to be deferred here?
func decompress(compression string, body io.Reader) (io.Reader, error) {
	printer.Debugf("Decompressing body using %s\n", compression)
	var dr io.Reader
	switch compression {
	case "gzip":
		if r, err := gzip.NewReader(body); err != nil {
			return nil, err
		} else {
			dr = r
		}
	case "deflate":
		dr = flate.NewReader(body)
	case "identity":
		dr = body
	case "br":
		dr = brotli.NewReader(body)
	default:
		return nil, errors.New("unsupported compression type")
	}
	return dr, nil
}

// Our only means of success seems to be reading all the way to the end.
// We limit the amount of space that can be produced that way (and the largest
// body we are willing to try.)
func attemptDecompress(body memview.MemView) (io.Reader, error) {
	if body.Len() > MaxFallbackInput {
		return nil, errors.New("body too large to attempt trial decompression")
	}

	for _, algorithm := range fallbackDecompressions {
		dr, err := decompress(algorithm, body.CreateReader())
		if err != nil {
			continue
		}
		limitReader := &io.LimitedReader{R: dr, N: MaxFallbackOutput}
		bufferedResult, err := ioutil.ReadAll(limitReader)
		if err == nil {
			return bytes.NewReader(bufferedResult), nil
		}
	}
	return nil, errors.New("unrecognized compression type")
}

// Handles character encoding and decompression.
func decodeBody(headers http.Header, body io.Reader, bodyDecompressed bool) (io.Reader, error) {
	// Handle decompression first.
	if !bodyDecompressed {
		compressions := headers[http.CanonicalHeaderKey("Content-Encoding")]
		if len(compressions) > 0 {
			printer.Debugf("Detected Content-Encoding header: %s\n", compressions)
		}
		// Content-Encoding is listed in the order applied, so we reverse the order
		// to decompress.
		for i := len(compressions) - 1; i >= 0; i-- {
			c := compressions[i]
			if b, err := decompress(c, body); err != nil {
				return nil, errors.Wrapf(err, "failed to decompress body with %s", c)
			} else {
				body = b
			}
		}
	}

	_, mtParams, err := mime.ParseMediaType(headers.Get("Content-Type"))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse MIME from Content-Type %q", headers.Get("Content-Type"))
	}

	// Convert non-UTF-8 bodies to UTF-8.
	// By default, we assume no charset indicates that the body is UTF-8 encoded
	// (which is a superset of ASCII) or the charset information is in the
	// payload, so the payload converter will take care of it (e.g. for text/xml).
	// See RFC 6657.
	if cs, ok := mtParams["charset"]; ok {
		enc, err := ianaindex.MIME.Encoding(cs)
		if err != nil {
			return nil, errors.Wrapf(err, `unsupported charset "%s"`, cs)
		}

		if n, _ := ianaindex.MIME.Name(enc); n != "UTF-8" {
			body = transform.NewReader(body, enc.NewDecoder())
		}
	}

	return body, nil
}

func limitedBufferBody(bodyStream io.Reader, limit int64) ([]byte, error) {
	body, err := ioutil.ReadAll(io.LimitReader(bodyStream, limit))
	if err != nil {
		return nil, errors.Wrap(err, "error reading body")
	}
	return body, nil
}

// Possible to return nil for both the data and error values. The data will be nil
// if the passed in body is length 0 or nil. This is not considered an error.
func parseBody(contentType string, bodyStream io.Reader, statusCode int) (*pb.Data, error) {
	mediaType, mediaParams, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse MIME from Content-Type %q", contentType)
	}

	// Handle multipart media types specially.
	switch mediaType {
	case "multipart/form-data":
		return parseMultipartBody("form-data", mediaParams["boundary"], bodyStream, statusCode)
	case "multipart/mixed":
		return parseMultipartBody("mixed", mediaParams["boundary"], bodyStream, statusCode)
	}

	// Otherwise, use media type to decide how to parse the body.
	// TODO: XML parsing
	// TODO: application/json-seq (RFC 7466)?
	// TODO: more text/* types
	var parseBodyDataAs pb.HTTPBody_ContentType
	switch mediaType {
	case "application/json":
		parseBodyDataAs = pb.HTTPBody_JSON
	case "application/x-www-form-urlencoded":
		parseBodyDataAs = pb.HTTPBody_FORM_URL_ENCODED
	case "application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml":
		parseBodyDataAs = pb.HTTPBody_YAML
	case "application/octet-stream":
		parseBodyDataAs = pb.HTTPBody_OCTET_STREAM
	case "text/plain", "text/csv":
		parseBodyDataAs = pb.HTTPBody_TEXT_PLAIN
	case "text/html":
		parseBodyDataAs = pb.HTTPBody_TEXT_HTML
	default:
		// Handle custom JSON-encoded media types.
		if strings.HasSuffix(mediaType, "+json") {
			parseBodyDataAs = pb.HTTPBody_JSON
		} else {
			parseBodyDataAs = pb.HTTPBody_OTHER
		}
	}

	var bodyData *pb.Data

	// Handle unstructured types, but use this local value to signal
	// errors so we can do the check just once
	var blobErr error = nil

	// Interpret as []byte
	handleAsBlob := func() {
		// Grab a small sample
		body, err := limitedBufferBody(bodyStream, SmallBodySample)
		if err != nil {
			blobErr = err
			return
		}
		bodyData = parseElem(body, spec_util.NO_INTERPRET_STRINGS)
	}
	// Interpret as string, optionally attempt to parse into another type
	handleAsString := func(interpret spec_util.InterpretStrings) {
		// Grab a small sample
		body, err := limitedBufferBody(bodyStream, SmallBodySample)
		if err != nil {
			blobErr = err
			return
		}
		bodyData = parseElem(string(body), interpret)
	}

	// Parse body.
	switch parseBodyDataAs {
	case pb.HTTPBody_JSON:
		bodyData, err = parseHTTPBodyJSON(bodyStream)
		if err != nil {
			return nil, errors.Wrapf(err, "could not parse JSON body")
		}
	case pb.HTTPBody_FORM_URL_ENCODED:
		body, err := limitedBufferBody(bodyStream, MaxBufferedBody)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, errors.Wrap(err, "could not parse URL-encoded body")
		}

		// Convert values to a map[string][]interface{} that parseElem operates on.
		m := make(map[string]interface{}, len(values))
		for k, vs := range values {
			// Make sure to not artificially create a list of values since this
			// affects the type we use in the witnesss and the generated spec.
			if len(vs) == 1 {
				m[k] = vs[0]
			} else {
				mvs := make([]interface{}, 0, len(vs))
				for _, v := range vs {
					mvs = append(mvs, v)
				}
				m[k] = mvs
			}
		}

		// In URL-encoded data, everything is represented as a string, so let's try
		// to be smart about re-interpreting values.
		bodyData = parseElem(m, spec_util.INTERPRET_STRINGS)
	case pb.HTTPBody_YAML:
		bodyData, err = parseHTTPBodyYAML(bodyStream)
		if err != nil {
			return nil, errors.Wrapf(err, "could not parse YAML body")
		}
	case pb.HTTPBody_OCTET_STREAM:
		handleAsBlob()
	case pb.HTTPBody_TEXT_PLAIN:
		// If the text is just a number, report its type
		handleAsString(spec_util.INTERPRET_STRINGS)
	case pb.HTTPBody_TEXT_HTML:
		handleAsString(spec_util.NO_INTERPRET_STRINGS)
	case pb.HTTPBody_OTHER:
		handleAsBlob()
	default:
		// If we get here, it means we added a new content type to the IR and
		// added it to the content type interpretation above, but forgot to
		// handle it here.  Print a debug warning and treat the body as a blob.
		printer.Debugf("skipping unknown content type: %v\n", parseBodyDataAs)
		handleAsBlob()
	}

	if blobErr != nil {
		// Error from handleAsBlob cases above
		return nil, blobErr
	}

	bodyMeta := &pb.HTTPBody{
		// The media type we parsed the body as.
		ContentType: parseBodyDataAs,

		// We're co-opting OtherType to always contain the original media type.
		OtherType: mediaType,
	}

	httpMeta := &pb.HTTPMeta{
		Location: &pb.HTTPMeta_Body{
			Body: bodyMeta,
		},
		ResponseCode: int32(statusCode),
	}
	bodyData.Meta = newDataMetaHTTPMeta(httpMeta)

	return bodyData, nil
}

func parseMultipartBody(multipartType string, boundary string, bodyStream io.Reader, statusCode int) (*pb.Data, error) {
	fields := map[string]*pb.Data{}
	r := multipart.NewReader(bodyStream, boundary)
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, errors.Wrap(err, "failed to read multipart part")
		}

		// By default, assume the content-type is text/plain, unless explicitly
		// specified. This corresponds to common use case of multipart bodies for
		// sending HTML form data.
		partContentType := part.Header.Get("Content-Type")
		if partContentType == "" {
			partContentType = "text/plain"
		}

		partData, err := parseBody(partContentType, part, statusCode)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to convert multipart field %q to data", part.FormName())
		}
		fields[part.FormName()] = partData
	}

	httpMeta := &pb.HTTPMeta{
		Location: &pb.HTTPMeta_Multipart{
			Multipart: &pb.HTTPMultipart{Type: multipartType},
		},
		ResponseCode: int32(statusCode),
	}

	return &pb.Data{
		Value: &pb.Data_Struct{
			Struct: &pb.Struct{Fields: fields},
		},
		Meta: newDataMetaHTTPMeta(httpMeta),
	}, nil
}

func parseRequest(req *akinet.HTTPRequest) (*pb.MethodMeta, []*pb.Data, optionals.Optional[string]) {
	datas := []*pb.Data{}
	noStatusCode := optionals.None[int]()
	datas = append(datas, parseQuery(req.URL)...)
	datas = append(datas, parseHeader(req.Header, noStatusCode)...)
	datas = append(datas, parseCookies(req.Cookies, noStatusCode)...)

	return parseMethodMeta(req), datas, parseLoadBalancer(req.Header)
}

func parseResponse(resp *akinet.HTTPResponse) []*pb.Data {
	datas := []*pb.Data{}
	statusCode := optionals.Some(resp.StatusCode)
	datas = append(datas, parseHeader(resp.Header, statusCode)...)
	datas = append(datas, parseCookies(resp.Cookies, statusCode)...)

	return datas
}

// Translate cookies into data objects.  optionals.None indicates that the
// header is in a request.
func parseCookies(cs []*http.Cookie, responseCodeOpt optionals.Optional[int]) []*pb.Data {
	// If the header is in a request, use 0 (default value) as the response code.
	responseCode := responseCodeOpt.GetOrDefault(0)

	datas := []*pb.Data{}
	for _, c := range cs {
		d := &pb.Data{
			Value: newDataPrimitive(categorizeStringToPrimitive(c.Value)),
			Meta:  newDataMetaCookie(&pb.HTTPCookie{Key: c.Name}, responseCode),
		}
		datas = append(datas, d)
	}
	return datas
}

// Extract the X-Forwarded-For header, if present
func parseLoadBalancer(header http.Header) optionals.Optional[string] {
	for k, vs := range header {
		switch strings.ToLower(k) {
		case "x-forwarded-for":
			if len(vs) > 0 {
				return optionals.Some(vs[0])
			}
		}
	}
	return optionals.None[string]()
}

// Translate headers to data objects.  optionals.None indicates that the header
// is in a request.
func parseHeader(header http.Header, responseCodeOpt optionals.Optional[int]) []*pb.Data {
	datas := []*pb.Data{}

	// If the header is in a request, use 0 (default value) as the response code.
	isRequest := responseCodeOpt.IsNone()
	responseCode := responseCodeOpt.GetOrDefault(0)

	// Sort the keys so there is a consistent ordering for resultant data structure
	ks := []string{}
	for k, _ := range header {
		ks = append(ks, k)
	}
	sort.Strings(ks)

	for _, k := range ks {
		var v string
		if len(header[k]) > 0 {
			// Only record each header once since we assume the same header always
			// contains data of the same type.
			v = header[k][0]
		} else {
			continue
		}

		switch strings.ToLower(k) {
		case "cookie", "set-cookie":
			// Cookies are parsed by parseCookies.
			continue
		case "content-type":
			// Handled by parseBody.
			continue
		case "x-forwarded-for":
			// Handled by parseLoadBalancer
			continue
		case "authorization":
			// If the authorization header is in the request, create an
			// HTTPAuth object.  Treat authorization headers in the response
			// the same as any other header.
			if isRequest {
				lv := strings.ToLower(v)

				var authType pb.HTTPAuth_HTTPAuthType
				var token string
				if strings.HasPrefix(lv, "bearer ") {
					authType = pb.HTTPAuth_BEARER
					token = v[len("bearer "):]
				} else if strings.HasPrefix(lv, "basic ") {
					authType = pb.HTTPAuth_BASIC
					token = v[len("basic "):]
				} else {
					authType = pb.HTTPAuth_UNKNOWN
					token = v
				}

				authData := &pb.Data{
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
				datas = append(datas, authData)

				continue
			}
		}

		d := &pb.Data{
			Value: newDataPrimitive(categorizeStringToPrimitive(v)),
			Meta:  newDataMetaHeader(&pb.HTTPHeader{Key: k}, responseCode),
		}
		datas = append(datas, d)
	}

	return datas
}

func parseQuery(url *url.URL) []*pb.Data {
	if url == nil {
		return nil
	}

	datas := []*pb.Data{}
	params := url.Query()

	// Sort the keys so there is a consistent ordering for resultant data structure
	ks := []string{}
	for k, _ := range params {
		ks = append(ks, k)
	}
	sort.Strings(ks)

	for _, k := range ks {
		vs := params[k]
		if len(vs) > 0 {
			// Only record each query param once since we assume the same param
			// always contains data of the same type.
			data := &pb.Data{
				Value: newDataPrimitive(categorizeStringToPrimitive(vs[0])),
				Meta:  newDataMetaQuery(&pb.HTTPQuery{Key: k}),
			}
			datas = append(datas, data)
		}
	}

	return datas
}

func parseMethodMeta(req *akinet.HTTPRequest) *pb.MethodMeta {
	path := ""
	if req.URL != nil {
		path = req.URL.Path
	}

	return &pb.MethodMeta{
		Meta: &pb.MethodMeta_Http{
			Http: &pb.HTTPMethodMeta{
				Method:       req.Method,
				PathTemplate: path,
				Host:         req.Host,
			},
		},
	}
}

func parseHTTPBodyJSON(stream io.Reader) (*pb.Data, error) {
	var top interface{}
	decoder := json.NewDecoder(newStripControlCharactersReader(stream))
	decoder.UseNumber()

	err := decoder.Decode(&top)

	if err != nil {
		return nil, errors.Wrapf(err, "couldn't parse JSON")
	}

	// JSON already distingishes string values from non-string values, so don't
	// interpret strings.
	return parseElem(top, spec_util.NO_INTERPRET_STRINGS), nil
}

func parseHTTPBodyYAML(stream io.Reader) (*pb.Data, error) {
	var top interface{}
	decoder := yaml.NewDecoder(stream)
	if err := decoder.Decode(&top); err != nil {
		return nil, errors.Wrapf(err, "couldn't parse YAML")
	}

	// Everything in YAML is a string, so let's be smart about re-interpreting
	// them as numbers and bools.
	return parseElem(top, spec_util.INTERPRET_STRINGS), nil
}

func parseElem(top interface{}, interpretStrings spec_util.InterpretStrings) *pb.Data {
	switch typedValue := top.(type) {
	case map[interface{}]interface{}:
		// YAML decoder uses interface{} as map keys, but we only support string
		// keys, so convert all keys to strings.
		m := make(map[string]interface{}, len(typedValue))
		for k, v := range typedValue {
			m[fmt.Sprintf("%v", k)] = v
		}
		return &pb.Data{Value: parseDataStruct(m, interpretStrings)}
	case map[string]interface{}:
		return &pb.Data{Value: parseDataStruct(typedValue, interpretStrings)}
	case []interface{}:
		return &pb.Data{Value: parseDataList(typedValue, interpretStrings)}
	case json.Number:
		if v, err := typedValue.Int64(); err == nil {
			top = v
		} else {
			// json.Number treats anything that does not fit in int64 as float64.
			// However, there's a possibility that the number could be uint64, so we
			// use our own categorizer.
			top = spec_util.CategorizeString(typedValue.String()).GoValue()
		}
	}

	// The switch statement above should have handled all non-primitive values.
	pv, err := spec_util.ToPrimitiveValue(top, interpretStrings)
	if err != nil {
		printer.Debugf("Unhandled non-primitive value of type %T, treating as none\n", top)
		return &pb.Data{
			Value: &pb.Data_Optional{
				Optional: &pb.Optional{
					Value: &pb.Optional_None{None: &pb.None{}},
				},
			},
		}
	}
	return &pb.Data{Value: newDataPrimitive(pv.ToProto())}
}

func parseDataStruct(elems map[string]interface{}, interpretStrings spec_util.InterpretStrings) *pb.Data_Struct {
	dataMap := make(map[string]*pb.Data)
	for k, v := range elems {
		dataMap[k] = parseElem(v, interpretStrings)
	}
	return &pb.Data_Struct{
		Struct: &pb.Struct{
			Fields: dataMap,
		},
	}
}

func parseDataList(elems []interface{}, interpretStrings spec_util.InterpretStrings) *pb.Data_List {
	dataList := []*pb.Data{}
	for _, v := range elems {
		dataList = append(dataList, parseElem(v, interpretStrings))
	}
	return &pb.Data_List{
		List: &pb.List{
			Elems: dataList,
		},
	}
}

// Note: We do not currently handle the 'bytes' primitive.
func categorizeStringToPrimitive(str string) *pb.Primitive {
	return spec_util.CategorizeString(str).ToProto()
}

// Spec construction helpers
func newDataPrimitive(p *pb.Primitive) *pb.Data_Primitive {
	return &pb.Data_Primitive{
		Primitive: p,
	}
}

// Returns an object with *pb.Data_Primitive if len(prims) == 1 or *pb.Data_OneOf
// if len(prims) > 1, with the conflict bit set.
// Does not set metadata.
func newConflictAwareDataValue(prims []*pb.Primitive) *pb.Data {
	if len(prims) == 0 {
		return nil
	} else if len(prims) == 1 {
		return &pb.Data{Value: newDataPrimitive(prims[0])}
	}
	var data []*pb.Data
	for _, prim := range prims {
		data = append(data, &pb.Data{
			Value: newDataPrimitive(prim),
		})
	}
	rv, err := spec_util.OneOf(data, true)
	if err != nil {
		printer.Debugf("hashing failed for: %s\n", err)
		return nil
	}
	return &pb.Data{Value: &pb.Data_Oneof{Oneof: rv}}
}

// DATA META
func newDataMetaHTTPMeta(httpM *pb.HTTPMeta) *pb.DataMeta {
	return &pb.DataMeta{
		Meta: &pb.DataMeta_Http{
			Http: httpM,
		},
	}
}

func newDataMetaQuery(query *pb.HTTPQuery) *pb.DataMeta {
	m := &pb.HTTPMeta{
		Location: &pb.HTTPMeta_Query{
			Query: query,
		},
	}
	return newDataMetaHTTPMeta(m)
}

func newDataMetaHeader(header *pb.HTTPHeader, responseCode int) *pb.DataMeta {
	m := &pb.HTTPMeta{
		Location: &pb.HTTPMeta_Header{
			Header: header,
		},
		ResponseCode: int32(responseCode),
	}
	return newDataMetaHTTPMeta(m)
}

func newDataMetaCookie(cookie *pb.HTTPCookie, responseCode int) *pb.DataMeta {
	m := &pb.HTTPMeta{
		Location: &pb.HTTPMeta_Cookie{
			Cookie: cookie,
		},
		ResponseCode: int32(responseCode),
	}
	return newDataMetaHTTPMeta(m)
}
