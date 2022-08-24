package pcap

import (
	"io/ioutil"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/gopacket"
	_ "github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-libs/akinet"
	akihttp "github.com/akitasoftware/akita-libs/akinet/http"
	"github.com/akitasoftware/akita-libs/buffer_pool"
	"github.com/akitasoftware/akita-libs/memview"
)

// Constants wrapped as functions because we can't read the testdata file at
// initialization time.
func simpleHTTPReq1() akinet.ParsedNetworkTraffic {
	return akinet.ParsedNetworkTraffic{
		SrcIP:   net.ParseIP("172.17.0.1"),
		SrcPort: 54854,
		DstIP:   net.ParseIP("172.17.0.2"),
		DstPort: 80,
		Content: akinet.HTTPRequest{
			Seq:        3027617259,
			Method:     "GET",
			ProtoMajor: 1,
			ProtoMinor: 1,
			URL:        &url.URL{Path: "/"},
			Host:       "localhost:8080",
			Header: map[string][]string{
				"Accept":                    []string{"text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8"},
				"Accept-Encoding":           []string{"gzip, deflate"},
				"Accept-Language":           []string{"en-US,en;q=0.5"},
				"Connection":                []string{"keep-alive"},
				"Dnt":                       []string{"1"},
				"Upgrade-Insecure-Requests": []string{"1"},
				"User-Agent":                []string{"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:75.0) Gecko/20100101 Firefox/75.0"},
			},
		},
	}
}

func simpleHTTPResp1() akinet.ParsedNetworkTraffic {
	return akinet.ParsedNetworkTraffic{
		SrcIP:   net.ParseIP("172.17.0.2"),
		SrcPort: 80,
		DstIP:   net.ParseIP("172.17.0.1"),
		DstPort: 54854,
		Content: akinet.HTTPResponse{
			Seq:        3027617259,
			StatusCode: 200,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header: map[string][]string{
				"Accept-Ranges":  []string{"bytes"},
				"Connection":     []string{"keep-alive"},
				"Content-Length": []string{"612"},
				"Content-Type":   []string{"text/html"},
				"Date":           []string{"Fri, 17 Apr 2020 03:45:06 GMT"},
				"Etag":           []string{`"5e5e6a8f-264"`},
				"Last-Modified":  []string{"Tue, 03 Mar 2020 14:32:47 GMT"},
				"Server":         []string{"nginx/1.17.9"},
			},
			Body: readFileOrDie("testdata/simple_http_response_body_1"),
		},
	}
}

func simpleHTTPReq2() akinet.ParsedNetworkTraffic {
	return akinet.ParsedNetworkTraffic{
		SrcIP:   net.ParseIP("172.17.0.1"),
		SrcPort: 54854,
		DstIP:   net.ParseIP("172.17.0.2"),
		DstPort: 80,
		Content: akinet.HTTPRequest{
			Seq:        3027618109,
			Method:     "GET",
			ProtoMajor: 1,
			ProtoMinor: 1,
			URL:        &url.URL{Path: "/favicon.ico"},
			Host:       "localhost:8080",
			Header: map[string][]string{
				"Accept":          []string{"image/webp,*/*"},
				"Accept-Encoding": []string{"gzip, deflate"},
				"Accept-Language": []string{"en-US,en;q=0.5"},
				"Connection":      []string{"keep-alive"},
				"Dnt":             []string{"1"},
				"User-Agent":      []string{"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:75.0) Gecko/20100101 Firefox/75.0"},
			},
		},
	}
}

func simpleHTTPResp2() akinet.ParsedNetworkTraffic {
	return akinet.ParsedNetworkTraffic{
		SrcIP:   net.ParseIP("172.17.0.2"),
		SrcPort: 80,
		DstIP:   net.ParseIP("172.17.0.1"),
		DstPort: 54854,
		Content: akinet.HTTPResponse{
			Seq:        3027618109,
			StatusCode: 404,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header: map[string][]string{
				"Connection":     []string{"keep-alive"},
				"Content-Length": []string{"153"},
				"Content-Type":   []string{"text/html"},
				"Date":           []string{"Fri, 17 Apr 2020 03:45:06 GMT"},
				"Server":         []string{"nginx/1.17.9"},
			},
			Body: readFileOrDie("testdata/simple_http_response_body_2"),
		},
	}
}

func readFileOrDie(path string) memview.MemView {
	bs, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return memview.New(bs)
}

func removeNonDeterministicField(p *akinet.ParsedNetworkTraffic) {
	p.ObservationTime = time.Time{}
	p.FinalPacketTime = time.Time{}
	switch c := p.Content.(type) {
	case akinet.HTTPRequest:
		c.StreamID = uuid.Nil
		p.Content = c
	case akinet.HTTPResponse:
		c.StreamID = uuid.Nil
		p.Content = c
	}
}

// pcapWrapper backed by a pcap file.
type filePcapWrapper string

func (f filePcapWrapper) capturePackets(done <-chan struct{}, _, _ string) (<-chan gopacket.Packet, error) {
	handle, err := pcap.OpenOffline(string(f))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %s", f)
	}

	out := make(chan gopacket.Packet)

	go func() {
		defer handle.Close()
		defer close(out)
		packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
		for packet := range packetSource.Packets() {
			select {
			case <-done:
				return
			case out <- packet:
			}
		}
	}()

	return out, nil
}

func (filePcapWrapper) getInterfaceAddrs(interfaceName string) ([]net.IP, error) {
	return nil, nil
}

func readFromPcapFile(file string, pool buffer_pool.BufferPool) ([]akinet.ParsedNetworkTraffic, error) {
	p := NewNetworkTrafficParser(1.0)
	p.pcap = filePcapWrapper(file)

	done := make(chan struct{})
	defer close(done)
	out, err := p.ParseFromInterface("fake", "", done, akihttp.NewHTTPRequestParserFactory(pool), akihttp.NewHTTPResponseParserFactory(pool))
	if err != nil {
		return nil, errors.Wrap(err, "ParseFromInterface failed")
	}

	collected := []akinet.ParsedNetworkTraffic{}
	for c := range out {
		// Remove TCP metadata, which was added after this test was written.
		if _, ignore := c.Content.(akinet.TCPPacketMetadata); ignore {
			c.Content.ReleaseBuffers()
			continue
		}

		removeNonDeterministicField(&c)
		collected = append(collected, c)
	}
	// The loop auto-terminates because filePcapWrapper closes its output channel
	// when EOF is reached.
	return collected, nil
}

func TestPcapHTTP(t *testing.T) {
	pool, err := buffer_pool.MakeBufferPool(1024*1024, 4*1024)
	if err != nil {
		t.Error(err)
	}

	testCases := []struct {
		name     string
		pcapFile string
		expected []akinet.ParsedNetworkTraffic
	}{
		{
			name:     "simple HTTP request response",
			pcapFile: "testdata/simple_http.pcap",
			expected: []akinet.ParsedNetworkTraffic{
				simpleHTTPReq1(),
				simpleHTTPResp1(),
			},
		},
		{
			name:     "simple HTTP request response with connection reuse",
			pcapFile: "testdata/simple_http_two.pcap",
			expected: []akinet.ParsedNetworkTraffic{
				simpleHTTPReq1(),
				simpleHTTPResp1(),
				simpleHTTPReq2(),
				simpleHTTPResp2(),
			},
		},
		// TODO: test "testdata/simple_http_two_with_noise.pcap"
	}

	for _, c := range testCases {
		t.Logf("testing %q", c.pcapFile)
		collected, err := readFromPcapFile(c.pcapFile, pool)
		if err != nil {
			t.Errorf("[%s] got unexpected error: %v", c.name, err)
		} else {

			// TODO: sort slice in cmp
			if diff := cmp.Diff(c.expected, collected, cmpopts.EquateEmpty(), cmpopts.IgnoreUnexported(akinet.HTTPRequest{}, akinet.HTTPResponse{})); diff != "" {
				t.Errorf("[%s] found diff: %s", c.name, diff)
			}
		}

		for _, pnt := range collected {
			pnt.Content.ReleaseBuffers()
		}
	}
}
