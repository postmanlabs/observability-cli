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

	"github.com/andybalholm/brotli"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"
	"gopkg.in/yaml.v2"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/pbhash"
	"github.com/akitasoftware/akita-libs/spec_util"
	pb "github.com/akitasoftware/akita-ir/go/api_spec"
)

var (
	unassignedHTTPID = &pb.MethodID{
		Name:    "",
		ApiType: pb.ApiType_HTTP_REST,
	}

	unknownHTTPMethodMeta = &pb.MethodMeta{
		Meta: &pb.MethodMeta_Http{
			Http: &pb.HTTPMethodMeta{
				Method:       "",
				PathTemplate: "",
				Host:         "",
			},
		},
	}

	sensitiveAnnotation = &pb.AkitaAnnotations{
		IsSensitive: true,
	}

	// List of compression algorithms to use as fallback if we can't decode the
	// body and no Content-Encoding header is set.
	// https://app.clubhouse.io/akita-software/story/1656
	fallbackDecompressions = []string{
		"deflate",
		"gzip",
		"br",
	}
)

type ParseAPISpecError string

func (pase ParseAPISpecError) Error() string {
	return string(pase)
}

func ParseHTTP(elem akinet.ParsedNetworkContent) (*PartialWitness, error) {
	var isRequest bool
	var rawBody []byte
	var bodyDecompressed bool
	var methodMeta *pb.MethodMeta
	var datas []*pb.Data
	var headers http.Header
	statusCode := 0

	var streamID uuid.UUID
	var seq int

	switch t := elem.(type) {
	case akinet.HTTPRequest:
		printer.Debugf("Parsing HTTP request: %+v\n", t)

		streamID = t.StreamID
		seq = t.Seq

		isRequest = true
		methodMeta, datas = parseRequest(&t)
		rawBody = t.Body
		bodyDecompressed = t.BodyDecompressed
		headers = t.Header
	case akinet.HTTPResponse:
		printer.Debugf("Parsing HTTP response: %+v\n", t)

		streamID = t.StreamID
		seq = t.Seq

		datas = parseResponse(&t)
		rawBody = t.Body
		bodyDecompressed = t.BodyDecompressed
		headers = t.Header
		statusCode = t.StatusCode
		methodMeta = unknownHTTPMethodMeta
	default:
		return nil, ParseAPISpecError("expected http message, got something else")
	}

	if len(rawBody) > 0 {
		body, err := decodeBody(headers, rawBody, bodyDecompressed)
		if err != nil {
			return nil, errors.Wrap(err, "failed to decode body")
		}

		contentType := headers.Get("Content-Type")
		bodyData, err := parseBody(contentType, body, statusCode)
		if err != nil {
			// Try common decompression algorithms to see if the body is compressed
			// but did not have Content-Encoding header.
			foundFallback := false
			printer.Debugf("Failed to parse body, attempting common decompressions\n")
			for _, fc := range fallbackDecompressions {
				if b, err := decompress(fc, body); err == nil {
					printer.Debugf("%s seems to work, attempting to parse body again\n", fc)
					body = b
					foundFallback = true
					break
				}
			}

			if foundFallback {
				bodyData, err = parseBody(contentType, body, statusCode)
			}
		}

		if err != nil {
			// Just log an error instead of returning an error so users can see the
			// other parts of the endpoint in the spec rather than an empty spec.
			// https://app.clubhouse.io/akita-software/story/1898/juan-s-payload-problem
			printer.Debugf("skipping unparsable body: %v\n", err)
		} else if bodyData != nil {
			datas = append(datas, bodyData)
		}
	}

	method := &pb.Method{Id: unassignedHTTPID, Meta: methodMeta}

	// Transform our array of datas into a map.
	// We assign sequential string IDs in order to provide a consistent ordering
	dataMap := map[string]*pb.Data{}
	for _, d := range datas {
		// Use the hash of the data proto as the key so we can deterministically
		// compare witnesses.
		k, err := pbhash.HashProto(d)
		if err != nil {
			return nil, errors.Wrap(err, "failed to hash data proto")
		}
		if _, collision := dataMap[k]; collision {
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
		Witness: &pb.Witness{Method: method},
		PairKey: toWitnessID(streamID, seq),
	}, nil
}

func decompress(compression string, body []byte) ([]byte, error) {
	printer.Debugf("Decompressing body using %s\n", compression)
	var dr io.Reader
	switch compression {
	case "gzip":
		if r, err := gzip.NewReader(bytes.NewReader(body)); err != nil {
			return nil, err
		} else {
			dr = r
		}
	case "deflate":
		dr = flate.NewReader(bytes.NewReader(body))
	case "identity":
		dr = bytes.NewReader(body)
	case "br":
		dr = brotli.NewReader(bytes.NewReader(body))
	default:
		return nil, errors.New("unsupported compression type")
	}
	return ioutil.ReadAll(dr)
}

// Handles character encoding and decompression.
func decodeBody(headers http.Header, body []byte, bodyDecompressed bool) ([]byte, error) {
	// Handle decompression first.
	if !bodyDecompressed {
		compressions := headers[http.CanonicalHeaderKey("Content-Encoding")]
		if len(compressions) > 0 {
			printer.Debugf("Detected Content-Encoding header: %s\n", compressions)
		}
		// Content-Encoding is listed in the order applied, so we reverse the order to
		// decompress.
		sort.Reverse(sort.StringSlice(compressions))
		for _, c := range compressions {
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
			utf8Body, _, err := transform.Bytes(enc.NewDecoder(), body)
			if err != nil {
				return nil, errors.Wrapf(err, `failed to convert %s to UTF-8`, cs)
			}
			body = utf8Body
		}
	}

	return body, nil
}

// Possible to return nil for both the data and error values. The data will be nil
// if the passed in body is length 0 or nil. This is not considered an error.
func parseBody(contentType string, body []byte, statusCode int) (*pb.Data, error) {
	if body == nil || len(body) == 0 {
		return nil, nil
	}

	mediaType, mediaParams, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse MIME from Content-Type %q", contentType)
	}

	var bodyData *pb.Data
	var pbContentType pb.HTTPBody_ContentType
	switch mediaType {
	case "application/json":
		bodyData, err = parseHTTPBodyJSON(body)
		if err != nil {
			return nil, errors.Wrapf(err, "could not parse JSON body")
		}
		pbContentType = pb.HTTPBody_JSON
	case "application/x-www-form-urlencoded":
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
		bodyData = parseElem(m)
		pbContentType = pb.HTTPBody_FORM_URL_ENCODED
	case "application/octet-stream":
		bodyData = parseElem(body)
		pbContentType = pb.HTTPBody_OCTET_STREAM
	case "text/plain":
		bodyData = parseElem(string(body))
		pbContentType = pb.HTTPBody_TEXT_PLAIN
	case "application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml":
		bodyData, err = parseHTTPBodyYAML(body)
		if err != nil {
			return nil, errors.Wrapf(err, "could not parse YAML body")
		}
		pbContentType = pb.HTTPBody_YAML
	case "multipart/form-data":
		return parseMultipartBody("form-data", mediaParams["boundary"], body, statusCode)
	case "multipart/mixed":
		return parseMultipartBody("mixed", mediaParams["boundary"], body, statusCode)
	default:
		return nil, ParseAPISpecError(fmt.Sprintf("could not parse body with media type: %s", mediaType))
	}

	httpMeta := &pb.HTTPMeta{
		Location: &pb.HTTPMeta_Body{
			Body: &pb.HTTPBody{
				ContentType: pbContentType,
			},
		},
		ResponseCode: int32(statusCode),
	}
	bodyData.Meta = newDataMetaHTTPMeta(httpMeta)

	return bodyData, nil
}

func parseMultipartBody(multipartType string, boundary string, body []byte, statusCode int) (*pb.Data, error) {
	fields := map[string]*pb.Data{}
	r := multipart.NewReader(bytes.NewReader(body), boundary)
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

		partBody, err := ioutil.ReadAll(part)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read multipart field %q", part.FormName())
		}

		partData, err := parseBody(partContentType, partBody, statusCode)
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

func parseRequest(req *akinet.HTTPRequest) (*pb.MethodMeta, []*pb.Data) {
	datas := []*pb.Data{}
	datas = append(datas, parseQuery(req.URL)...)
	datas = append(datas, parseHeader(req.Header, 0)...)
	datas = append(datas, parseCookies(req.Cookies, 0)...)

	return parseMethodMeta(req), datas
}

func parseResponse(resp *akinet.HTTPResponse) []*pb.Data {
	datas := []*pb.Data{}
	datas = append(datas, parseHeader(resp.Header, resp.StatusCode)...)
	datas = append(datas, parseCookies(resp.Cookies, resp.StatusCode)...)

	return datas
}

func parseCookies(cs []*http.Cookie, responseCode int) []*pb.Data {
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

func parseHeader(header http.Header, responseCode int) []*pb.Data {
	datas := []*pb.Data{}

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
			// Cookies are parsed by parseHeader.
			continue
		case "content-type":
			// Handled by parseBody.
			continue
		case "authorization":
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

func parseHTTPBodyJSON(raw []byte) (*pb.Data, error) {
	var top interface{}
	decoder := json.NewDecoder(bytes.NewBuffer(raw))
	decoder.UseNumber()

	err := decoder.Decode(&top)

	if err != nil {
		return nil, errors.Wrapf(err, "couldn't parse JSON")
	}

	return parseElem(top), nil
}

func parseHTTPBodyYAML(raw []byte) (*pb.Data, error) {
	var top interface{}
	decoder := yaml.NewDecoder(bytes.NewBuffer(raw))
	if err := decoder.Decode(&top); err != nil {
		return nil, errors.Wrapf(err, "couldn't parse YAML")
	}
	return parseElem(top), nil
}

func parseElem(top interface{}) *pb.Data {
	switch typedValue := top.(type) {
	case map[interface{}]interface{}:
		// YAML decoder uses interface{} as map keys, but we only support string
		// keys, so convert all keys to strings.
		m := make(map[string]interface{}, len(typedValue))
		for k, v := range typedValue {
			m[fmt.Sprintf("%v", k)] = v
		}
		return &pb.Data{Value: parseDataStruct(m)}
	case map[string]interface{}:
		return &pb.Data{Value: parseDataStruct(typedValue)}
	case []interface{}:
		return &pb.Data{Value: parseDataList(typedValue)}
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
	pv, err := spec_util.ToPrimitiveValue(top)
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

func parseDataStruct(elems map[string]interface{}) *pb.Data_Struct {
	dataMap := make(map[string]*pb.Data)
	for k, v := range elems {
		dataMap[k] = parseElem(v)
	}
	return &pb.Data_Struct{
		Struct: &pb.Struct{
			Fields: dataMap,
		},
	}
}

func parseDataList(elems []interface{}) *pb.Data_List {
	dataList := []*pb.Data{}
	for _, v := range elems {
		dataList = append(dataList, parseElem(v))
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
