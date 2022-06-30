package trace

import (
	"math"
	"strconv"

	"github.com/OneOfOne/xxhash"
	"github.com/akitasoftware/akita-libs/client_telemetry"

	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
)

type Collector interface {
	// Hands new data from network to the collector. The implementation may choose
	// to process them asynchronously (e.g. to wait for the response to a
	// corresponding request).
	// Implementations should only return error if the error is unrecoverable and
	// the whole process should stop immediately.
	Process(akinet.ParsedNetworkTraffic) error

	// Implementations must complete processing all requests/responses before
	// returning.
	Close() error
}

// Wraps a Collector and performs sampling.
type SamplingCollector struct {
	// A sample is used if a coin flip is below this threshold.
	sampleThreshold float64

	collector Collector
}

// Wraps a collector and performs sampling. Returns the collector itself if the
// given sampleRate is 1.0.
func NewSamplingCollector(sampleRate float64, collector Collector) Collector {
	if sampleRate == 1.0 {
		return collector
	}

	return &SamplingCollector{
		sampleThreshold: float64(math.MaxUint32) * sampleRate,
		collector:       collector,
	}
}

// Sample based on stream ID and seq so a pair of request and response are
// either both selected or both excluded.
func (sc *SamplingCollector) includeSample(key string) bool {
	h := xxhash.New32()
	h.WriteString(key)
	return float64(h.Sum32()) < sc.sampleThreshold
}

func (sc *SamplingCollector) Process(t akinet.ParsedNetworkTraffic) error {
	var key string
	switch c := t.Content.(type) {
	case akinet.HTTPRequest:
		key = c.StreamID.String() + strconv.Itoa(c.Seq)
	case akinet.HTTPResponse:
		key = c.StreamID.String() + strconv.Itoa(c.Seq)
	case akinet.TCPConnectionMetadata:
		key = akid.String(c.ConnectionID)
	case akinet.TLSHandshakeMetadata:
		key = akid.String(c.ConnectionID)
	default:
		key = ""
	}
	if sc.includeSample(key) {
		return sc.collector.Process(t)
	}
	return nil
}

func (sc *SamplingCollector) Close() error {
	return sc.collector.Close()
}

// Filters out CLI's own traffic to Akita APIs.
type UserTrafficCollector struct {
	Collector Collector
}

func (sc *UserTrafficCollector) Process(t akinet.ParsedNetworkTraffic) error {
	if !util.ContainsCLITraffic(t) {
		return sc.Collector.Process(t)
	}
	return nil
}

func (sc *UserTrafficCollector) Close() error {
	return sc.Collector.Close()
}

// This is a shim to add packet counts based on payload type.
type PacketCountCollector struct {
	PacketCounts PacketCountConsumer
	Collector    Collector
}

func (pc *PacketCountCollector) Process(t akinet.ParsedNetworkTraffic) error {
	switch t.Content.(type) {
	case akinet.HTTPRequest:
		pc.PacketCounts.Update(client_telemetry.PacketCounts{
			Interface:    t.Interface,
			SrcPort:      t.SrcPort,
			DstPort:      t.DstPort,
			HTTPRequests: 1,
		})
	case akinet.HTTPResponse:
		pc.PacketCounts.Update(client_telemetry.PacketCounts{
			Interface:     t.Interface,
			SrcPort:       t.SrcPort,
			DstPort:       t.DstPort,
			HTTPResponses: 1,
		})
	case akinet.TCPPacketMetadata, akinet.TCPConnectionMetadata:
		// Don't count TCP metadata.
	case akinet.TLSHandshakeMetadata:
		// Don't count TLS metadata.
	default:
		pc.PacketCounts.Update(client_telemetry.PacketCounts{
			Interface: t.Interface,
			SrcPort:   t.SrcPort,
			DstPort:   t.DstPort,
			Unparsed:  1,
		})
	}
	return pc.Collector.Process(t)
}

func (pc *PacketCountCollector) Close() error {
	return pc.Collector.Close()
}
