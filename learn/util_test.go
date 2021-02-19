package learn

import (
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/pbhash"
	"github.com/akitasoftware/akita-libs/spec_util"
	as "github.com/akitasoftware/akita-ir/go/api_spec"
	pb "github.com/akitasoftware/akita-ir/go/api_spec"
)

const (
	applicationJSON = "application/json"
)

func runComp(pt *parseTest) error {
	result, err := ParseHTTP(pt.testContent)
	if err != nil {
		return errors.Wrap(err, "failed to parse")
	}

	method := result.Witness.Method

	if diff := cmp.Diff(pt.expectedMethod, method, cmp.Comparer(proto.Equal)); diff != "" {
		return errors.Errorf("got different diff than expected: %s", diff)
	}

	if err = reflexMarshalTest(method); err != nil {
		return errors.Wrap(err, "marshal error")
	}

	return nil
}

func reflexMarshalTest(m *as.Method) error {
	marshalBytes, err := proto.Marshal(m)
	if err != nil {
		return errors.Wrap(err, "could not marshal method")
	}

	var newM as.Method
	err = proto.Unmarshal(marshalBytes, &newM)
	if err != nil {
		return errors.Wrap(err, "could not unmarshal method")
	}
	return nil
}

func newTestHTTPRequest(
	method string,
	urlStr string,
	body []byte,
	contentType string,
	inHeader map[string][]string,
	cookies []*http.Cookie) akinet.HTTPRequest {

	h := http.Header(inHeader)
	h.Add("Content-Type", contentType)

	testURL, _ := url.Parse(urlStr)
	return akinet.HTTPRequest{
		Seq:        1,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Method:     method,
		URL:        testURL,
		Host:       "www.akitasoftware.com",
		Body:       body,
		Cookies:    cookies,
		Header:     h,
	}
}

func newTestHTTPResponse(
	statusCode int,
	body []byte,
	contentType string,
	inHeader map[string][]string,
	cookies []*http.Cookie) akinet.HTTPResponse {

	h := http.Header(inHeader)
	h.Add("Content-Type", contentType)

	return akinet.HTTPResponse{
		Seq:        1,
		StatusCode: statusCode,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     h,
		Body:       body,
		Cookies:    cookies,
	}
}

func newMethod(argsData []*pb.Data, responsesData []*pb.Data, meta *pb.MethodMeta) *pb.Method {
	m := &pb.Method{Meta: meta, Id: unassignedHTTPID}
	if argsData != nil {
		m.Args = map[string]*pb.Data{}
		for _, v := range argsData {
			k, err := pbhash.HashProto(v)
			if err != nil {
				panic(err)
			}
			m.Args[k] = v
		}
	}
	if responsesData != nil {
		m.Responses = map[string]*pb.Data{}
		for _, v := range responsesData {
			k, err := pbhash.HashProto(v)
			if err != nil {
				panic(err)
			}
			m.Responses[k] = v
		}
	}
	return m
}

func newAuth(authType pb.HTTPAuth_HTTPAuthType, token string) *pb.Data {
	meta := &pb.HTTPMeta{
		Location: &pb.HTTPMeta_Auth{
			Auth: &pb.HTTPAuth{Type: authType},
		},
	}

	data := dataFromPrimitive(spec_util.CategorizeString(token).Obfuscate().ToProto())
	data.Meta = newDataMeta(meta)
	return data
}

func newDataHeader(k string, responseCode int, prim *pb.Primitive, sensitive bool) *pb.Data {
	meta := &pb.HTTPMeta{
		Location: &pb.HTTPMeta_Header{
			Header: &pb.HTTPHeader{Key: k},
		},
		ResponseCode: int32(responseCode),
	}

	data := dataFromPrimitive(annotateIfSensitiveForTest(sensitive, prim))
	data.Meta = newDataMeta(meta)
	return data
}

func newDataCookie(k string, responseCode int, sensitive bool, value string) *pb.Data {
	meta := &pb.HTTPMeta{
		Location: &pb.HTTPMeta_Cookie{
			Cookie: &pb.HTTPCookie{Key: k},
		},
		ResponseCode: int32(responseCode),
	}
	data := dataFromPrimitive(annotateIfSensitiveForTest(sensitive, spec_util.NewPrimitiveString(value)))
	data.Meta = newDataMeta(meta)
	return data
}

func newDataMeta(httpM *pb.HTTPMeta) *pb.DataMeta {
	return &pb.DataMeta{
		Meta: &pb.DataMeta_Http{
			Http: httpM,
		},
	}
}

func newDataQuery(key string, prim *pb.Primitive) *pb.Data {
	return &pb.Data{
		Value: &pb.Data_Primitive{Primitive: prim},
		Meta: newDataMeta(&pb.HTTPMeta{
			Location: &pb.HTTPMeta_Query{
				Query: &pb.HTTPQuery{Key: key},
			},
		}),
	}
}

func dataFromStruct(fields map[string]*pb.Data) *pb.Data {
	return &pb.Data{Value: &pb.Data_Struct{Struct: &pb.Struct{Fields: fields}}}
}
func dataFromList(elems ...*pb.Data) *pb.Data {
	return &pb.Data{Value: &pb.Data_List{List: &pb.List{Elems: elems}}}
}

func dataFromPrimitive(p *pb.Primitive) *pb.Data {
	return &pb.Data{Value: &pb.Data_Primitive{Primitive: p}}
}

func newBodyDataMeta(responseCode int, contentType pb.HTTPBody_ContentType) *pb.DataMeta {
	return newDataMeta(&pb.HTTPMeta{
		Location: &pb.HTTPMeta_Body{
			Body: &pb.HTTPBody{
				ContentType: contentType,
			},
		},
		ResponseCode: int32(responseCode),
	})
}

func annotateIfSensitiveForTest(sensitive bool, prim *pb.Primitive) *pb.Primitive {
	/* (CNS, 7/28/2020): SuperLearn currently does not support the
	* x-akita-is-sensitive annotation.

	if sensitive {
		primCopy := proto.Clone(prim).(*pb.Primitive)
		primCopy.AkitaAnnotations = sensitiveAnnotation
		return primCopy
	}
	*/

	return prim
}
