package learn

import (
	"bytes"
	"compress/flate"
	"net/http"
	"strings"
	"testing"

	as "github.com/akitasoftware/akita-ir/go/api_spec"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/spec_util"

	"github.com/spf13/viper"
)

const (
	validLuhn    = "378734493671000"
	fakePassword = "akitaSecretPassword"
)

var deflatedBody bytes.Buffer

func init() {
	dw, err := flate.NewWriter(&deflatedBody, flate.BestCompression)
	if err != nil {
		panic(err)
	}
	dw.Write([]byte(`{"34302ecf": "this is prince"}`))
	dw.Close()
}

var testBodyDict = `
{
  "name": "prince",
  "number_teeth": 9000,
  "dog": true,
  "canadian_social_insurance_number": "378734493671000",
  "homes": ["burbank, ca", "jeuno, ak", "versailles"],
  "jobs" : {
    "statistician" : "senior director",
    "plumber" : "emeritus lecturer",
    "oyster farmer" : "embarrassment"
  }
}
`

var testMultipartFormData = strings.Join([]string{
	"--b9580db\r\n",
	"Content-Disposition: form-data; name=\"field1\"\r\n",
	"\r\n",
	"value1\r\n",
	"--b9580db\r\n",
	"Content-Disposition: form-data; name=\"field2\"\r\n",
	"Content-Type: application/json\r\n",
	"\r\n",
	`{"foo": "bar", "baz": 123}` + "\r\n",
	"--b9580db--",
}, "")

func newTestBodySpec(statusCode int) *as.Data {
	return newTestBodySpecContentType("application/json", statusCode)
}

func newTestBodySpecContentType(contentType string, statusCode int) *as.Data {
	return newTestBodySpecFromStruct(statusCode, as.HTTPBody_JSON, contentType, map[string]*as.Data{
		"name":                             dataFromPrimitive(spec_util.NewPrimitiveString("prince")),
		"number_teeth":                     dataFromPrimitive(spec_util.NewPrimitiveInt64(9000)),
		"dog":                              dataFromPrimitive(spec_util.NewPrimitiveBool(true)),
		"canadian_social_insurance_number": dataFromPrimitive(annotateIfSensitiveForTest(true, spec_util.NewPrimitiveString("378734493671000"))),
		"homes": dataFromList(
			dataFromPrimitive(spec_util.NewPrimitiveString("burbank, ca")),
			dataFromPrimitive(spec_util.NewPrimitiveString("jeuno, ak")),
			dataFromPrimitive(spec_util.NewPrimitiveString("versailles")),
		),
		"jobs": dataFromStruct(map[string]*as.Data{
			"statistician":  dataFromPrimitive(spec_util.NewPrimitiveString("senior director")),
			"plumber":       dataFromPrimitive(spec_util.NewPrimitiveString("emeritus lecturer")),
			"oyster farmer": dataFromPrimitive(spec_util.NewPrimitiveString("embarrassment")),
		}),
	})
}

func newTestBodySpecFromStruct(statusCode int, contentType as.HTTPBody_ContentType, originalContentType string, s map[string]*as.Data) *as.Data {
	return newTestBodySpecFromData(statusCode, contentType, originalContentType, dataFromStruct(s))
}

func newTestBodySpecFromData(statusCode int, contentType as.HTTPBody_ContentType, originalContentType string, d *as.Data) *as.Data {
	d.Meta = newBodyDataMeta(statusCode, contentType, originalContentType)
	return d
}

func newTestMultipartFormData(statusCode int) *as.Data {
	f1 := dataFromPrimitive(spec_util.NewPrimitiveString("value1"))

	return &as.Data{
		Value: &as.Data_Struct{
			Struct: &as.Struct{
				Fields: map[string]*as.Data{
					"field1": newTestBodySpecFromData(statusCode, as.HTTPBody_TEXT_PLAIN, "text/plain", f1),
					"field2": newTestBodySpecFromStruct(statusCode, as.HTTPBody_JSON, "application/json", map[string]*as.Data{
						"foo": dataFromPrimitive(spec_util.NewPrimitiveString("bar")),
						"baz": dataFromPrimitive(spec_util.NewPrimitiveInt64(123)),
					}),
				},
			},
		},

		Meta: &as.DataMeta{
			Meta: &as.DataMeta_Http{
				&as.HTTPMeta{
					Location: &as.HTTPMeta_Multipart{
						&as.HTTPMultipart{Type: "form-data"},
					},
				},
			},
		},
	}
}

type parseTest struct {
	name           string
	expectedMethod *as.Method
	expectedMeta   *as.MethodMeta
	testContent    akinet.ParsedNetworkContent
}

func TestParseHTTPRequest(t *testing.T) {
	standardMethodMeta := &as.MethodMeta{
		Meta: &as.MethodMeta_Http{
			Http: &as.HTTPMethodMeta{
				Method:       "GET",
				PathTemplate: "",
				Host:         "www.akitasoftware.com",
			},
		},
	}
	standardMethodPostMeta := &as.MethodMeta{
		Meta: &as.MethodMeta_Http{
			Http: &as.HTTPMethodMeta{
				Method:       "POST",
				PathTemplate: "",
				Host:         "www.akitasoftware.com",
			},
		},
	}

	tests := []*parseTest{
		&parseTest{
			name: "body test 1",
			testContent: newTestHTTPRequest(
				"GET",
				"https://www.akitasoftware.com",
				[]byte(testBodyDict),
				applicationJSON,
				map[string][]string{},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod([]*as.Data{newTestBodySpec(0)}, nil, standardMethodMeta),
		},
		&parseTest{
			name: "custom JSON-encoded content type test 1",
			testContent: newTestHTTPRequest(
				"POST",
				"https://www.akitasoftware.com",
				[]byte(testBodyDict),
				"application/custom+json",
				map[string][]string{},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod([]*as.Data{newTestBodySpecContentType("application/custom+json", 0)}, nil, standardMethodPostMeta),
		},
		&parseTest{
			name: "query test 1",
			testContent: newTestHTTPRequest(
				"GET",
				// We should only record the multi-value lemurs query param once.
				"https://www.akitasoftware.com?weeble=grommit&wozzle=42&qux=-72.3&lemurs=378734493671000&lemurs=938245723",
				nil,
				applicationJSON,
				map[string][]string{},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				[]*as.Data{
					newDataQuery("lemurs", annotateIfSensitiveForTest(true, spec_util.NewPrimitiveInt64(378734493671000))),
					newDataQuery("qux", spec_util.NewPrimitiveDouble(-72.3)),
					newDataQuery("weeble", spec_util.NewPrimitiveString("grommit")),
					newDataQuery("wozzle", spec_util.NewPrimitiveInt64(42)),
				},
				nil,
				standardMethodMeta,
			),
		},
		&parseTest{
			name: "headers test 1",
			testContent: newTestHTTPRequest(
				"GET",
				"https://www.akitasoftware.com",
				nil,
				applicationJSON,
				map[string][]string{
					"X-Clandestine": []string{"Sneaky"},
					// We should only record the multi-value X-Top-Secret-Level header
					// once.
					"X-Top-Secret-Level":  []string{"super ultra mega", "marginal"},
					"X-Secret-Handshakes": []string{validLuhn},
				},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				[]*as.Data{
					newDataHeader("X-Clandestine", 0, spec_util.NewPrimitiveString("Sneaky"), false),
					newDataHeader("X-Secret-Handshakes", 0, spec_util.NewPrimitiveInt64(378734493671000), true),
					newDataHeader("X-Top-Secret-Level", 0, spec_util.NewPrimitiveString("super ultra mega"), false),
				},
				nil,
				standardMethodMeta,
			),
		},
		&parseTest{
			name: "cookies test 1",
			testContent: newTestHTTPRequest(
				"GET",
				"https://www.akitasoftware.com",
				nil,
				applicationJSON,
				map[string][]string{},
				[]*http.Cookie{
					&http.Cookie{Name: "furniture", Value: "rococo"},
					&http.Cookie{Name: "art", Value: "baroque"},
					&http.Cookie{Name: "music", Value: "harpischord"},
					&http.Cookie{Name: "heraldry", Value: fakePassword},
				},
			),
			expectedMethod: newMethod(
				[]*as.Data{
					newDataCookie("furniture", 0, false, "rococo"),
					newDataCookie("art", 0, false, "baroque"),
					newDataCookie("music", 0, false, "harpischord"),
					newDataCookie("heraldry", 0, true, fakePassword),
				},
				nil,
				standardMethodMeta,
			),
		},
		&parseTest{
			name: "resp body test 1",
			testContent: newTestHTTPResponse(
				200,
				[]byte(testBodyDict),
				applicationJSON,
				map[string][]string{},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{newTestBodySpec(200)},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			name: "resp header test 1",
			testContent: newTestHTTPResponse(
				500,
				nil,
				applicationJSON,
				map[string][]string{
					// We should only record the multi-value X-Codename once.
					"X-Codename":       []string{"Operation Paperclip", "Operation Ivy"},
					"X-FullOfLampreys": []string{fakePassword},
					"X-Charming-Level": []string{"extreme"},
				},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{
					newDataHeader("X-Charming-Level", 500, spec_util.NewPrimitiveString("extreme"), false),
					newDataHeader("X-Codename", 500, spec_util.NewPrimitiveString("Operation Paperclip"), false),
					newDataHeader("X-FullOfLampreys", 500, spec_util.NewPrimitiveString(fakePassword), true),
				},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			name: "resp cookies test 1",
			testContent: newTestHTTPResponse(
				404,
				nil,
				applicationJSON,
				map[string][]string{},
				[]*http.Cookie{
					&http.Cookie{Name: "furniture", Value: "mid-century modern"},
					&http.Cookie{Name: "origin", Value: "scandinavian"},
					&http.Cookie{Name: "manufacture", Value: "high pressure extrusion"},
					&http.Cookie{Name: "concave", Value: fakePassword},
				},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{
					newDataCookie("furniture", 404, false, "mid-century modern"),
					newDataCookie("origin", 404, false, "scandinavian"),
					newDataCookie("manufacture", 404, false, "high pressure extrusion"),
					newDataCookie("concave", 404, true, fakePassword),
				},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			name: "URL encoded body",
			testContent: newTestHTTPResponse(
				200,
				[]byte("prince=a+good+doggo&pineapple=0&pineapple=1"),
				"application/x-www-form-urlencoded",
				map[string][]string{},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{
					newTestBodySpecFromStruct(
						200,
						as.HTTPBody_FORM_URL_ENCODED,
						"application/x-www-form-urlencoded",
						map[string]*as.Data{
							"prince": dataFromPrimitive(spec_util.NewPrimitiveString("a good doggo")),
							"pineapple": dataFromList(
								dataFromPrimitive(spec_util.NewPrimitiveInt64(0)),
								dataFromPrimitive(spec_util.NewPrimitiveInt64(1)),
							),
						},
					),
				},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			name: "octet-stream body",
			testContent: newTestHTTPResponse(
				200,
				[]byte("prince is a good boy"),
				"application/octet-stream",
				map[string][]string{},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{
					newTestBodySpecFromData(
						200,
						as.HTTPBody_OCTET_STREAM,
						"application/octet-stream",
						dataFromPrimitive(spec_util.NewPrimitiveBytes([]byte("prince is a good boy"))),
					),
				},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			name: "yaml body",
			testContent: newTestHTTPResponse(
				200,
				[]byte(`
prince:
  - bread
  - eat
`),
				"application/x-yaml",
				map[string][]string{},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{
					newTestBodySpecFromStruct(
						200,
						as.HTTPBody_YAML,
						"application/x-yaml",
						map[string]*as.Data{
							"prince": dataFromList(
								dataFromPrimitive(spec_util.NewPrimitiveString("bread")),
								dataFromPrimitive(spec_util.NewPrimitiveString("eat")),
							),
						},
					),
				},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			name: "body charset - latin1",
			testContent: newTestHTTPResponse(
				200,
				[]byte("{\"\x66\xFC\x72\": \"\x66\xFC\x72\"}"), // {"für": "für"}
				"application/json; charset=latin1",
				map[string][]string{},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{
					newTestBodySpecFromStruct(
						200,
						as.HTTPBody_JSON,
						"application/json",
						map[string]*as.Data{
							"für": dataFromPrimitive(spec_util.NewPrimitiveString("für")),
						},
					),
				},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			name: "deflated body with content-encoding header",
			testContent: newTestHTTPResponse(
				200,
				deflatedBody.Bytes(),
				"application/json",
				map[string][]string{"Content-Encoding": {"deflate"}},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{
					newDataHeader("Content-Encoding", 200, spec_util.NewPrimitiveString("deflate"), false),
					newTestBodySpecFromStruct(
						200,
						as.HTTPBody_JSON,
						"application/json",
						map[string]*as.Data{
							"34302ecf": dataFromPrimitive(spec_util.NewPrimitiveString("this is prince")),
						},
					),
				},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			// Test our fallback mechanism for auto-detecting compressed bodies.
			// https://app.clubhouse.io/akita-software/story/1656
			name: "deflated body without content-encoding header",
			testContent: newTestHTTPResponse(
				200,
				deflatedBody.Bytes(),
				"application/json",
				map[string][]string{},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{
					newTestBodySpecFromStruct(
						200,
						as.HTTPBody_JSON,
						"application/json",
						map[string]*as.Data{
							"34302ecf": dataFromPrimitive(spec_util.NewPrimitiveString("this is prince")),
						},
					),
				},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			// Log error and skip the body if we can't parse it, instead of aborting
			// the whole endpoint.
			// https://app.clubhouse.io/akita-software/story/1898/juan-s-payload-problem
			name: "skip body if unable to parse",
			testContent: newTestHTTPResponse(
				200,
				[]byte("I am not JSON"),
				"application/json",
				map[string][]string{
					"X-Charming-Level": []string{"extreme"},
				},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod(
				nil,
				[]*as.Data{
					newDataHeader("X-Charming-Level", 200, spec_util.NewPrimitiveString("extreme"), false),
				},
				UnknownHTTPMethodMeta(),
			),
		},
		&parseTest{
			name: "auth header",
			testContent: newTestHTTPRequest(
				"GET",
				"https://www.akitasoftware.com",
				nil,
				applicationJSON,
				map[string][]string{
					"Authorization": []string{"basic 38aa49900bbe50228ad9b56b5549dcce3c36912a"},
				},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod([]*as.Data{
				newAuth(as.HTTPAuth_BASIC, "38aa49900bbe50228ad9b56b5549dcce3c36912a"),
			}, nil, standardMethodMeta),
		},
		&parseTest{
			name: "multipart/form-data",
			testContent: newTestHTTPRequest(
				"POST",
				"https://www.akitasoftware.com",
				[]byte(testMultipartFormData),
				applicationJSON,
				map[string][]string{
					"Content-Type": []string{"multipart/form-data;boundary=b9580db"},
				},
				[]*http.Cookie{},
			),
			expectedMethod: newMethod([]*as.Data{
				newTestMultipartFormData(0),
			}, nil, standardMethodPostMeta),
		},
		&parseTest{
			name: "duplicate cookies",
			testContent: newTestHTTPRequest(
				"GET",
				"https://www.akitasoftware.com",
				nil,
				applicationJSON,
				map[string][]string{},
				[]*http.Cookie{
					&http.Cookie{Name: "furniture", Value: "rococo"},
					&http.Cookie{Name: "furniture", Value: "rococo"},
				},
			),
			expectedMethod: newMethod(
				[]*as.Data{
					newDataCookie("furniture", 0, false, "rococo"),
				},
				nil,
				standardMethodMeta,
			),
		},
	}

	for _, pt := range tests {
		err := runComp(pt)
		if err != nil {
			t.Fatalf("error in test: %s \\ %v ", pt.name, err)
		}
	}
}

// Make sure the fallbackDecompression list is supported by the decompress
// method.
func TestFallbackDecompressionList(t *testing.T) {
	junk := []byte("abcdefghijklmnopqrstuvwxyz")
	for _, fc := range fallbackDecompressions {
		_, err := decompress(fc, bytes.NewReader(junk))
		if err != nil && err.Error() == "unsupported compression type" {
			t.Errorf("%s is not supported by decompress", fc)
		}
	}
}

func TestFailingParse(t *testing.T) {
	// Look at the debug messages
	// TODO: is there any way to grab them programatically?  Install a new Stderr, maybe?
	viper.Set("debug", true)

	testCases := []struct {
		Name        string
		TestContent akinet.ParsedNetworkContent
	}{
		{
			Name: "deflate error",
			TestContent: newTestHTTPResponse(
				200,
				[]byte("xxxyzzy"),
				"application/json",
				map[string][]string{"Content-Encoding": {"deflate"}},
				[]*http.Cookie{},
			),
		},
		{
			Name: "json error",
			TestContent: newTestHTTPResponse(
				200,
				[]byte("{oops_no_quotes: 3}"),
				"application/json",
				map[string][]string{},
				[]*http.Cookie{},
			),
		},
	}
	for _, tc := range testCases {
		_, err := ParseHTTP(tc.TestContent)
		// The recognizable portions of the response are returned anyway
		if err != nil {
			t.Errorf("%q returned an error %q", tc.Name, err)
		}
	}

	viper.Set("debug", false)

}
