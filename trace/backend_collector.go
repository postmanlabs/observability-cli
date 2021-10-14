package trace

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
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

func (r witnessWithInfo) toReport(dir kgxapi.NetworkDirection) (*kgxapi.WitnessReport, error) {
	// Hash algorithm defined in
	// https://docs.google.com/document/d/1ZANeoLTnsO10DcuzsAt6PBCt2MWLYW8oeu_A6d9bTJk/edit#heading=h.tbvm9waph6eu
	hash := ir_hash.HashWitnessToString(r.witness)

	b, err := proto.Marshal(r.witness)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal witness proto")
	}

	return &kgxapi.WitnessReport{
		Direction:       dir,
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

// Sends witnesses up to akita cloud.
type BackendCollector struct {
	serviceID      akid.ServiceID
	learnSessionID akid.LearnSessionID
	learnClient    rest.LearnClient
	dir            kgxapi.NetworkDirection

	// Cache un-paired partial witnesses by pair key.
	// akid.WitnessID -> *witnessWithInfo
	pairCache sync.Map

	// Batch of REST witnesses pending upload.
	uploadWitnessBatch *batcher.InMemory

	// Batch of TCP-connection reports pending upload.
	uploadTCPConnectionReportBatch *batcher.InMemory

	// Channel controlling periodic cache flush
	flushDone chan struct{}

	plugins []plugin.AkitaPlugin
}

func NewBackendCollector(svc akid.ServiceID,
	lrn akid.LearnSessionID, lc rest.LearnClient, dir kgxapi.NetworkDirection,
	plugins []plugin.AkitaPlugin) Collector {
	col := &BackendCollector{
		serviceID:      svc,
		learnSessionID: lrn,
		learnClient:    lc,
		dir:            dir,
		flushDone:      make(chan struct{}),
		plugins:        plugins,
	}

	col.uploadWitnessBatch = batcher.NewInMemory(
		col.uploadWitnesses,
		uploadBatchMaxSize,
		uploadBatchFlushDuration)
	col.uploadTCPConnectionReportBatch = batcher.NewInMemory(
		col.uploadTCPConnectionReports,
		uploadBatchMaxSize,
		uploadBatchFlushDuration,
	)

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
	if tcp.Direction == akinet.DestToSource {
		srcAddr, srcPort, dstAddr, dstPort = dstAddr, dstPort, srcAddr, srcPort
	}

	c.uploadTCPConnectionReportBatch.Add(&kgxapi.TCPConnectionReport{
		ID:             tcp.ConnectionID,
		SrcAddr:        srcAddr,
		SrcPort:        uint16(srcPort),
		DestAddr:       dstAddr,
		DestPort:       uint16(dstPort),
		FirstObserved:  packet.ObservationTime,
		LastObserved:   packet.FinalPacketTime,
		DirectionKnown: tcp.Direction != akinet.UnknownTCPConnectionDirection,
		EndState:       tcp.EndState,
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
	c.uploadWitnessBatch.Add(w)
}

func (c *BackendCollector) Close() error {
	close(c.flushDone)
	c.flushPairCache(time.Now())
	c.uploadWitnessBatch.Close()
	c.uploadTCPConnectionReportBatch.Close()
	return nil
}

func (c *BackendCollector) uploadWitnesses(in []interface{}) {
	reports := make([]*kgxapi.WitnessReport, 0, len(in))
	for _, i := range in {
		w := i.(*witnessWithInfo)
		r, err := w.toReport(c.dir)
		if err == nil {
			reports = append(reports, r)
		} else {
			printer.Warningf("Failed to convert witness to report: %v\n", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := c.learnClient.ReportWitnesses(ctx, c.learnSessionID, reports)
	if err != nil {
		switch e := err.(type) {
		case rest.HTTPError:
			if e.StatusCode == http.StatusTooManyRequests {
				// XXX Not all commands that call into this code have a --rate-limit
				// option.
				err = errors.Wrap(err, "your witness uploads are being throttled. Akita will generate partial results. Try reducing the --rate-limit value to avoid this.")
			}
		}
		printer.Warningf("Failed to upload witnesses: %v\n", err)
	}
	printer.Debugf("Uploaded %d witnesses\n", len(in))
}

func (c *BackendCollector) uploadTCPConnectionReports(in []interface{}) {
	reports := make([]*kgxapi.TCPConnectionReport, 0, len(in))
	for _, elt := range in {
		reports = append(reports, elt.(*kgxapi.TCPConnectionReport))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := c.learnClient.ReportTCPConnections(ctx, c.learnSessionID, reports)
	if err != nil {
		switch e := err.(type) {
		case rest.HTTPError:
			if e.StatusCode == http.StatusTooManyRequests {
				// XXX Not all commands that call into this code have a --rate-limit
				// option.
				// TODO Would be nice to re-queue these reports and try again later.
				err = errors.Wrap(err, "your witness uploads are being throttled. Akita will generate partial results. Try reducing the --rate-limit value to avoid this.")
			}
		}
		printer.Warningf("Failed to upload connection reports: %v\n", err)
	}
	printer.Debugf("Uploaded %d connection reports\n", len(in))
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
