package pcap

import (
	"testing"
	"time"

	"github.com/akitasoftware/akita-libs/akinet"
	akihttp "github.com/akitasoftware/akita-libs/akinet/http"
	"github.com/akitasoftware/akita-libs/buffer_pool"
	"github.com/google/gopacket"
)

func TestHTTPRequestTimes(t *testing.T) {
	pool, err := buffer_pool.MakeBufferPool(1024*1024, 4*1024)
	if err != nil {
		t.Error(err)
	}

	// create an abnormal trace with one byte every 100ms
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	input := "POST /foo HTTP/1.1\r\nHost: example.com\r\nContent-Length: 9\r\n\r\nfoobarbaz"

	pkts := make([]gopacket.Packet, len(input))
	for i, _ := range pkts {
		pkts[i] = CreatePacketWithSeq(ip1, ip2, port1, port2, []byte{input[i]}, uint32(i))
		pkts[i].Metadata().CaptureInfo.Timestamp = startTime.Add(time.Duration(i*100) * time.Millisecond)
	}

	closeChan := make(chan struct{})
	defer close(closeChan)
	out, err := setupParseFromInterface(fakePcap(pkts), closeChan, akihttp.NewHTTPRequestParserFactory(pool))
	if err != nil {
		t.Errorf("unexpected error setting up listener: %v", err)
		return
	}

	var actual []akinet.ParsedNetworkTraffic
	for pnt := range out {
		actual = append(actual, pnt)
	}

	if len(actual) != 1 {
		t.Fatalf("Expected 1 parsed object, got %v", len(actual))
	}

	if actual[0].ObservationTime != startTime {
		t.Fatalf("Observation time %v does not match %v", actual[0].ObservationTime, startTime)
	}

	endTime := startTime.Add(time.Duration((len(input)-1)*100) * time.Millisecond)
	if actual[0].FinalPacketTime != endTime {
		t.Fatalf("Final time %v does not match %v", actual[0].FinalPacketTime, endTime)
	}

	for _, pnt := range actual {
		pnt.Content.ReleaseBuffers()
	}
}
