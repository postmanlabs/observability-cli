package trace

import (
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	pb "github.com/akitasoftware/akita-ir/go/api_spec"

	mockrest "github.com/akitasoftware/akita-cli/rest/mock"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/spec_util"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
)

var (
	fakeSvc = akid.NewServiceID(uuid.Must(uuid.Parse("8b2cf196-87fe-4e53-a6b9-1452d7efb863")))
	fakeLrn = akid.NewLearnSessionID(uuid.Must(uuid.Parse("2b5dd735-9fc0-4365-93e8-74bf86d3f853")))
)

type witnessRecorder struct {
	witnesses []*pb.Witness
}

// Record a call to LearnClient.ReportWitnesses
func (wr *witnessRecorder) recordReportWitnesses(args ...interface{}) {
	reports := args[2].([]*kgxapi.WitnessReport)
	for _, r := range reports {
		bs, err := base64.URLEncoding.DecodeString(r.WitnessProto)
		if err != nil {
			panic(err)
		}

		w := &pb.Witness{}
		if err := proto.Unmarshal(bs, w); err != nil {
			panic(err)
		}
		wr.witnesses = append(wr.witnesses, w)
	}
}

// Make sure we obfuscate values before uploading.
func TestObfuscate(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockClient := mockrest.NewMockLearnClient(ctrl)
	defer ctrl.Finish()

	var rec witnessRecorder
	mockClient.
		EXPECT().
		ReportWitnesses(gomock.Any(), gomock.Any(), gomock.Any()).
		Do(rec.recordReportWitnesses).
		AnyTimes().
		Return(nil)

	streamID := uuid.New()
	req := akinet.ParsedNetworkTraffic{
		Content: akinet.HTTPRequest{
			StreamID: streamID,
			Seq:      1203,
			Method:   "POST",
			URL: &url.URL{
				Path: "/v1/doggos",
			},
			Host: "example.com",
			Header: map[string][]string{
				"Content-Type": {"application/json"},
			},
			Body: []byte(`{"name": "prince", "number": 6119717375543385000}`),
		},
	}

	resp := akinet.ParsedNetworkTraffic{
		Content: akinet.HTTPResponse{
			StreamID:   streamID,
			Seq:        1203,
			StatusCode: 200,
			Header: map[string][]string{
				"Content-Type": {"application/json"},
			},
			Body: []byte(`{"homes": ["burbank, ca", "jeuno, ak", "versailles"]}`),
		},
	}

	col := NewBackendCollector(fakeSvc, fakeLrn, mockClient, kgxapi.Inbound, nil)
	assert.NoError(t, col.Process(req))
	assert.NoError(t, col.Process(resp))
	assert.NoError(t, col.Close())

	expectedWitnesses := []*pb.Witness{
		&pb.Witness{
			Method: &pb.Method{
				Id: &pb.MethodID{
					ApiType: pb.ApiType_HTTP_REST,
				},
				Args: map[string]*pb.Data{
					"BuVeSzMAimw=": newTestBodySpecFromStruct(0, pb.HTTPBody_JSON, map[string]*pb.Data{
						"name": dataFromPrimitive(spec_util.NewPrimitiveString(
							"lgkXNsG1k7-cxarrFoo-MmhjoRP3YOXV3C0k6rrKy2A="),
						),
						"number": dataFromPrimitive(spec_util.NewPrimitiveInt64(8191886688482385179)),
					}),
				},
				Responses: map[string]*pb.Data{
					"Ye1yQe9ylz0=": newTestBodySpecFromStruct(200, pb.HTTPBody_JSON, map[string]*pb.Data{
						"homes": dataFromList(
							dataFromPrimitive(spec_util.NewPrimitiveString(
								"hZwXhGMIxoOotCt-Cu4toMf9g8CpZnOdUe3bPxEn_Sg="),
							),
							dataFromPrimitive(spec_util.NewPrimitiveString(
								"ESrSgUKxboEvBrJrfm6z9xQKnegYZ_YUcOaZ4il3ytY="),
							),
							dataFromPrimitive(spec_util.NewPrimitiveString(
								"M7hhiIKycdahIkwhrHNl9gDQSxzbbcElQMyvDOPiJhI="),
							),
						),
					}),
				},
				Meta: &pb.MethodMeta{
					Meta: &pb.MethodMeta_Http{
						Http: &pb.HTTPMethodMeta{
							Method:       "POST",
							PathTemplate: "/v1/doggos",
							Host:         "example.com",
						},
					},
				},
			},
		},
	}

	for i := range expectedWitnesses {
		expected := proto.MarshalTextString(expectedWitnesses[i])
		actual := proto.MarshalTextString(rec.witnesses[i])
		assert.Equal(t, expected, actual)
	}
}

func dataFromPrimitive(p *pb.Primitive) *pb.Data {
	return &pb.Data{Value: &pb.Data_Primitive{Primitive: p}}
}

func dataFromStruct(fields map[string]*pb.Data) *pb.Data {
	return &pb.Data{Value: &pb.Data_Struct{Struct: &pb.Struct{Fields: fields}}}
}

func dataFromList(elems ...*pb.Data) *pb.Data {
	return &pb.Data{Value: &pb.Data_List{List: &pb.List{Elems: elems}}}
}

func newTestBodySpecFromStruct(statusCode int, contentType pb.HTTPBody_ContentType, s map[string]*pb.Data) *pb.Data {
	return newTestBodySpecFromData(statusCode, contentType, dataFromStruct(s))
}

func newTestBodySpecFromData(statusCode int, contentType pb.HTTPBody_ContentType, d *pb.Data) *pb.Data {
	d.Meta = newBodyDataMeta(statusCode, contentType)
	return d
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

func newDataMeta(httpM *pb.HTTPMeta) *pb.DataMeta {
	return &pb.DataMeta{
		Meta: &pb.DataMeta_Http{
			Http: httpM,
		},
	}
}
