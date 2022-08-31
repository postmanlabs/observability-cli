package learn

import (
	"net/url"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"

	pb "github.com/akitasoftware/akita-ir/go/api_spec"

	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/memview"
	"github.com/akitasoftware/akita-libs/spec_util"
)

func NewPrimitiveString(v string) *pb.Primitive {
	return &pb.Primitive{
		Value: &pb.Primitive_StringValue{
			StringValue: &pb.String{Value: v},
		},
	}
}

var (
	helloReq = akinet.ParsedNetworkTraffic{
		Content: akinet.HTTPRequest{
			StreamID: uuid.MustParse("8ae69ff5-33a4-427d-b0e8-7f993c6052c3"),
			Seq:      1203,
			Method:   "POST",
			URL:      &url.URL{Path: "/hello"},
			Host:     "www.akitasoftware.com",
			Header: map[string][]string{
				"Content-Type": []string{"application/json"},
			},
			Body: memview.New([]byte(`"2020-07-22T18:55:17.911Z"`)),
		},
	}

	helloResp = akinet.ParsedNetworkTraffic{
		Content: akinet.HTTPResponse{
			StreamID:   uuid.MustParse("8ae69ff5-33a4-427d-b0e8-7f993c6052c3"),
			Seq:        1203,
			StatusCode: 200,
			Header: map[string][]string{
				"Content-Type": []string{"application/json"},
			},
			Body: memview.New([]byte(`{"you said": "2020-07-22T18:55:17.911Z"}`)),
		},
	}

	helloArgData = &pb.Data{
		Value: &pb.Data_Primitive{
			Primitive: NewPrimitiveString(
				"2020-07-22T18:55:17.911Z",
			),
		},
		Meta: &pb.DataMeta{
			Meta: &pb.DataMeta_Http{
				&pb.HTTPMeta{
					Location: &pb.HTTPMeta_Body{
						Body: &pb.HTTPBody{
							ContentType: pb.HTTPBody_JSON,
							OtherType:   "application/json",
						},
					},
				},
			},
		},
	}

	helloRespData = &pb.Data{
		Value: &pb.Data_Struct{
			Struct: &pb.Struct{
				Fields: map[string]*pb.Data{
					"you said": &pb.Data{
						Value: &pb.Data_Primitive{
							Primitive: NewPrimitiveString(
								"2020-07-22T18:55:17.911Z",
							),
						},
					},
				},
			},
		},
		Meta: &pb.DataMeta{
			Meta: &pb.DataMeta_Http{
				&pb.HTTPMeta{
					ResponseCode: 200,
					Location: &pb.HTTPMeta_Body{
						Body: &pb.HTTPBody{
							ContentType: pb.HTTPBody_JSON,
							OtherType:   "application/json",
						},
					},
				},
			},
		},
	}

	helloMeta = &pb.MethodMeta{
		Meta: &pb.MethodMeta_Http{
			Http: &pb.HTTPMethodMeta{
				Method:       "POST",
				PathTemplate: "/hello",
				Host:         "www.akitasoftware.com",
			},
		},
	}

	largeIntResp = akinet.ParsedNetworkTraffic{
		Content: akinet.HTTPResponse{
			StreamID:   uuid.MustParse("ee4e11b3-514f-4b0c-b00b-6ea61b7a3701"),
			Seq:        1203,
			StatusCode: 200,
			Header: map[string][]string{
				"Content-Type": []string{"application/json"},
			},
			Body: memview.New([]byte(`
{
	"num1": 6119717375543385000,
	"num2": 14201265876841261000
}
`)),
		},
	}

	largeIntRespData = &pb.Data{
		Value: &pb.Data_Struct{
			Struct: &pb.Struct{
				Fields: map[string]*pb.Data{
					"num1": &pb.Data{
						Value: &pb.Data_Primitive{
							Primitive: spec_util.NewPrimitiveInt64(6119717375543385000),
						},
					},
					"num2": &pb.Data{
						Value: &pb.Data_Primitive{
							Primitive: spec_util.NewPrimitiveUint64(14201265876841261000),
						},
					},
				},
			},
		},
		Meta: &pb.DataMeta{
			Meta: &pb.DataMeta_Http{
				&pb.HTTPMeta{
					ResponseCode: 200,
					Location: &pb.HTTPMeta_Body{
						Body: &pb.HTTPBody{
							ContentType: pb.HTTPBody_JSON,
							OtherType:   "application/json",
						},
					},
				},
			},
		},
	}
)

func dummySensitiveDataMatcher(string) bool {
	return false
}

func witnessLess(w1, w2 *pb.Witness) bool {
	return proto.MarshalTextString(w1) < proto.MarshalTextString(w2)
}

func TestPairPartialWitness(t *testing.T) {
	testCases := []struct {
		name     string
		inputs   []akinet.ParsedNetworkTraffic
		expected []*pb.Witness
	}{
		{
			name:   "http request and response",
			inputs: []akinet.ParsedNetworkTraffic{helloReq, helloResp},
			expected: []*pb.Witness{
				&pb.Witness{
					Method: newMethod([]*pb.Data{helloArgData}, []*pb.Data{helloRespData}, helloMeta),
				},
			},
		},
		{
			// Observing the response first should not affect pairing.
			name:   "http response before request",
			inputs: []akinet.ParsedNetworkTraffic{helloResp, helloReq},
			expected: []*pb.Witness{
				&pb.Witness{
					Method: newMethod([]*pb.Data{helloArgData}, []*pb.Data{helloRespData}, helloMeta),
				},
			},
		},
		{
			name:   "unpaired http response flushed as partial witness",
			inputs: []akinet.ParsedNetworkTraffic{helloResp},
			expected: []*pb.Witness{
				&pb.Witness{
					Method: newMethod(nil, []*pb.Data{helloRespData}, UnknownHTTPMethodMeta()),
				},
			},
		},
		{
			// Make sure we treat numbers that fit in uint64 as uint64 instead of
			// float64.
			// See https://github.com/akitasoftware/superstar/pull/600
			name:   "large int",
			inputs: []akinet.ParsedNetworkTraffic{largeIntResp},
			expected: []*pb.Witness{
				&pb.Witness{
					Method: newMethod(nil, []*pb.Data{largeIntRespData}, UnknownHTTPMethodMeta()),
				},
			},
		},
	}

	for _, c := range testCases {
		inputChan := make(chan akinet.ParsedNetworkTraffic)
		go func() {
			for _, i := range c.inputs {
				inputChan <- i
			}
			close(inputChan)
		}()

		resultChan := startLearning(inputChan)
		actual := []*pb.Witness{}
		for r := range resultChan {
			actual = append(actual, r.witness)
		}

		if diff := cmp.Diff(c.expected, actual, cmp.Comparer(proto.Equal), cmpopts.SortSlices(witnessLess)); diff != "" {
			t.Errorf("[%s] found diff: %s", c.name, diff)
		}
	}
}
