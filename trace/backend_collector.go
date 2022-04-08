package trace

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/learn"
	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	pb "github.com/akitasoftware/akita-ir/go/api_spec"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/batcher"
	"github.com/akitasoftware/akita-libs/spec_util"
	"github.com/akitasoftware/akita-libs/spec_util/ir_hash"
)

const (
	// We stop trying to pair partial witnesses older than pairCacheExpiration.
	pairCacheExpiration = time.Minute

	// How often we clean out stale partial witnesses from pairCache.
	pairCacheCleanupInterval = 30 * time.Second

	// Max size per upload batch.
	uploadBatchMaxSize = 120

	// How often to flush the upload batch.
	uploadBatchFlushDuration = 30 * time.Second
)

type witnessWithInfo struct {
	srcIP           net.IP
	srcPort         uint16
	dstIP           net.IP
	dstPort         uint16
	observationTime time.Time
	id              akid.WitnessID
	requestEnd      time.Time
	responseStart   time.Time

	witness *pb.Witness
}

func (r witnessWithInfo) toReport() (*kgxapi.WitnessReport, error) {
	// Hash algorithm defined in
	// https://docs.google.com/document/d/1ZANeoLTnsO10DcuzsAt6PBCt2MWLYW8oeu_A6d9bTJk/edit#heading=h.tbvm9waph6eu
	hash := ir_hash.HashWitnessToString(r.witness)

	b, err := proto.Marshal(r.witness)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal witness proto")
	}

	return &kgxapi.WitnessReport{
		Direction:       kgxapi.Inbound,
		OriginAddr:      r.srcIP,
		OriginPort:      r.srcPort,
		DestinationAddr: r.dstIP,
		DestinationPort: r.dstPort,

		WitnessProto:      base64.URLEncoding.EncodeToString(b),
		ClientWitnessTime: r.observationTime,
		Hash:              hash,
		ID:                r.id,
	}, nil
}

func (w *witnessWithInfo) recordTimestamp(isRequest bool, t akinet.ParsedNetworkTraffic) {
	if isRequest {
		w.requestEnd = t.FinalPacketTime
	} else {
		w.responseStart = t.ObservationTime
	}

}

func (w witnessWithInfo) computeProcessingLatency(isRequest bool, t akinet.ParsedNetworkTraffic) {
	// Processing latency is the time from the last packet of the request,
	// to the first packet of the response.
	requestEnd := w.requestEnd
	responseStart := t.ObservationTime

	// handle arrival in opposite order
	if isRequest {
		requestEnd = t.FinalPacketTime
		responseStart = w.responseStart
	}

	// Missing data, leave as default value in protobuf
	if requestEnd.IsZero() || responseStart.IsZero() {
		return
	}

	// HTTPMethodMetadata only for now
	if meta := spec_util.HTTPMetaFromMethod(w.witness.Method); meta != nil {
		latency := responseStart.Sub(requestEnd)
		meta.ProcessingLatency = float32(latency.Microseconds()) / 1000.0
	}
}

// An additional method supported by the backend collector to switch
type LearnSessionCollector interface {
	Collector

	SwitchLearnSession(akid.LearnSessionID)
}

// Sends witnesses up to akita cloud.
type BackendCollector struct {
	serviceID      akid.ServiceID
	learnSessionID akid.LearnSessionID
	learnClient    rest.LearnClient

	// Cache un-paired partial witnesses by pair key.
	// akid.WitnessID -> *witnessWithInfo
	pairCache sync.Map

	// Batch of reports (witnesses, TCP-connection reports, etc.) pending upload.
	uploadReportBatch *batcher.InMemory

	// Channel controlling periodic cache flush
	flushDone chan struct{}

	// Mutex protecting learnSessionID
	learnSessionMutex sync.Mutex

	plugins []plugin.AkitaPlugin
}

var _ LearnSessionCollector = (*BackendCollector)(nil)

func NewBackendCollector(svc akid.ServiceID,
	lrn akid.LearnSessionID, lc rest.LearnClient,
	plugins []plugin.AkitaPlugin) Collector {
	col := &BackendCollector{
		serviceID:      svc,
		learnSessionID: lrn,
		learnClient:    lc,
		flushDone:      make(chan struct{}),
		plugins:        plugins,
	}

	col.uploadReportBatch = batcher.NewInMemory(
		col.uploadReports,
		uploadBatchMaxSize,
		uploadBatchFlushDuration)

	go col.periodicFlush()

	return col
}

func (c *BackendCollector) Process(t akinet.ParsedNetworkTraffic) error {
	var isRequest bool
	var partial *learn.PartialWitness
	var parseHTTPErr error
	switch content := t.Content.(type) {
	case akinet.HTTPRequest:
		isRequest = true
		partial, parseHTTPErr = learn.ParseHTTP(content)
	case akinet.HTTPResponse:
		partial, parseHTTPErr = learn.ParseHTTP(content)
	case akinet.TCPConnectionMetadata:
		return c.processTCPConnection(t, content)
	case akinet.TLSHandshakeMetadata:
		return c.processTLSHandshake(content)
	default:
		// Non-HTTP traffic not handled
		return nil
	}

	if parseHTTPErr != nil {
		printer.Debugf("Failed to parse HTTP, skipping: %v\n", parseHTTPErr)
		return nil
	}

	if val, ok := c.pairCache.LoadAndDelete(partial.PairKey); ok {
		pair := val.(*witnessWithInfo)

		// Combine the pair, merging the result into the existing item
		// rather than the new partial.
		learn.MergeWitness(pair.witness, partial.Witness)
		pair.computeProcessingLatency(isRequest, t)

		// If partial is the request, flip the src/dst in the pair before
		// reporting.
		if isRequest {
			pair.srcIP, pair.dstIP = pair.dstIP, pair.srcIP
			pair.srcPort, pair.dstPort = pair.dstPort, pair.srcPort
		}

		c.queueUpload(pair)
	} else {
		// Store the partial witness for now, waiting for its pair or a
		// flush timeout.
		w := &witnessWithInfo{
			srcIP:           t.SrcIP,
			srcPort:         uint16(t.SrcPort),
			dstIP:           t.DstIP,
			dstPort:         uint16(t.DstPort),
			witness:         partial.Witness,
			observationTime: t.ObservationTime,
			id:              partial.PairKey,
		}
		// Store whichever timestamp brackets the processing interval.
		w.recordTimestamp(isRequest, t)
		c.pairCache.Store(partial.PairKey, w)

	}
	return nil
}

func (c *BackendCollector) processTCPConnection(packet akinet.ParsedNetworkTraffic, tcp akinet.TCPConnectionMetadata) error {
	srcAddr, srcPort, dstAddr, dstPort := packet.SrcIP, packet.SrcPort, packet.DstIP, packet.DstPort
	if tcp.Initiator == akinet.DestInitiator {
		srcAddr, srcPort, dstAddr, dstPort = dstAddr, dstPort, srcAddr, srcPort
	}

	c.uploadReportBatch.Add(&kgxapi.TCPConnectionReport{
		ID:             tcp.ConnectionID,
		SrcAddr:        srcAddr,
		SrcPort:        uint16(srcPort),
		DestAddr:       dstAddr,
		DestPort:       uint16(dstPort),
		FirstObserved:  packet.ObservationTime,
		LastObserved:   packet.FinalPacketTime,
		InitiatorKnown: tcp.Initiator != akinet.UnknownTCPConnectionInitiator,
		EndState:       tcp.EndState,
	})
	return nil
}

func (c *BackendCollector) processTLSHandshake(tls akinet.TLSHandshakeMetadata) error {
	c.uploadReportBatch.Add(&kgxapi.TLSHandshakeReport{
		ID:                      tls.ConnectionID,
		Version:                 tls.Version,
		SNIHostname:             tls.SNIHostname,
		SupportedProtocols:      tls.SupportedProtocols,
		SelectedProtocol:        tls.SelectedProtocol,
		SubjectAlternativeNames: tls.SubjectAlternativeNames,
	})
	return nil
}

func (c *BackendCollector) queueUpload(w *witnessWithInfo) {
	for _, p := range c.plugins {
		if err := p.Transform(w.witness.GetMethod()); err != nil {
			// Only upload if plugins did not return error.
			printer.Errorf("plugin %q returned error, skipping: %v", p.Name(), err)
			return
		}
	}

	// Obfuscate the original value so type inference engine can use it on the
	// backend without revealing the actual value.
	obfuscate(w.witness.GetMethod())
	c.uploadReportBatch.Add(w)
}

func (c *BackendCollector) Close() error {
	close(c.flushDone)
	c.flushPairCache(time.Now())
	c.uploadReportBatch.Close()
	return nil
}

func (c *BackendCollector) SwitchLearnSession(session akid.LearnSessionID) {
	c.learnSessionMutex.Lock()
	defer c.learnSessionMutex.Unlock()
	c.learnSessionID = session
}

func (c *BackendCollector) getLearnSession() akid.LearnSessionID {
	c.learnSessionMutex.Lock()
	defer c.learnSessionMutex.Unlock()
	return c.learnSessionID
}

func (c *BackendCollector) uploadReports(in []interface{}) {
	witnesses := make([]*kgxapi.WitnessReport, 0, len(in))
	tcpConnections := make([]*kgxapi.TCPConnectionReport, 0, len(in))
	tlsHandshakes := make([]*kgxapi.TLSHandshakeReport, 0, len(in))
	for _, i := range in {
		switch i := i.(type) {
		case *witnessWithInfo:
			r, err := i.toReport()
			if err == nil {
				witnesses = append(witnesses, r)
			} else {
				printer.Warningf("Failed to convert witness to report: %v\n", err)
			}

		case *kgxapi.TCPConnectionReport:
			tcpConnections = append(tcpConnections, i)

		case *kgxapi.TLSHandshakeReport:
			tlsHandshakes = append(tlsHandshakes, i)

		default:
			printer.Warningf("Ignoring unknown report type %s. (This is an internal error.)\n", reflect.TypeOf(i).Name())
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	upload := kgxapi.UploadReportsRequest{
		Witnesses:      witnesses,
		TCPConnections: tcpConnections,
		TLSHandshakes:  tlsHandshakes,
	}
	err := c.learnClient.AsyncReportsUpload(ctx, c.getLearnSession(), &upload)
	if err != nil {
		switch e := err.(type) {
		case rest.HTTPError:
			if e.StatusCode == http.StatusTooManyRequests {
				// XXX Not all commands that call into this code have a --rate-limit
				// option.
				err = errors.Wrap(err, "your witness uploads are being throttled. Akita will generate partial results. Try reducing the --rate-limit value to avoid this.")
			}
		}
		printer.Warningf("Failed to upload to Akita Cloud: %v\n", err)
	}
	printer.Debugf("Uploaded %d witnesses and %d TCP connection reports\n", len(witnesses), len(tcpConnections))
}

func (c *BackendCollector) periodicFlush() {
	ticker := time.NewTicker(pairCacheCleanupInterval)

	for true {
		select {
		case <-ticker.C:
			c.flushPairCache(time.Now().Add(-1 * pairCacheExpiration))
		case <-c.flushDone:
			ticker.Stop()
			return
		}
	}
}

func (c *BackendCollector) flushPairCache(cutoffTime time.Time) {
	c.pairCache.Range(func(k, v interface{}) bool {
		e := v.(*witnessWithInfo)
		if e.observationTime.Before(cutoffTime) {
			c.queueUpload(e)
			c.pairCache.Delete(k)
		}
		return true
	})
}
