package apispec

import (
	"time"

	"github.com/google/martian/v3/har"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	hl "github.com/akitasoftware/akita-cli/har_loader"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/sampled_err"
)

// Extract witnesses from a local HAR file and send them to the collector.
func ProcessHAR(inboundCol, outboundCol trace.Collector, p string) error {
	harContent, err := hl.LoadCustomHARFromFile(p)
	if err != nil {
		return errors.Wrapf(err, "failed to load HAR file %s", p)
	}

	col := inboundCol
	if harContent.AkitaExt.Outbound {
		col = outboundCol
	}

	successCount, errs := parseFromHAR(col, harContent.Log)
	if errs.TotalCount > 0 {
		entriesCount := len(harContent.Log.Entries)
		printer.Stderr.Warningf("Encountered errors with %d HAR file entries.\n", entriesCount-successCount)
		printer.Stderr.Warningf("Akita will ignore entries with errors and generate a spec from the %d entries successfully processed.\n", successCount)

		printer.Stderr.Warningf("Sample errors:\n")
		for _, e := range errs.Samples {
			printer.Stderr.Warningf("\t- %s\n", e)
		}
	}

	return nil
}

// ReconstructedTimestamps holds times we can use to fill in
// a ParsedNetworkTraffic, in such a way that the reported
// latencies in the Witness will end up the same as in the HAR
// file.  (Because I couldn't think of a good way to pass
// the measurements through the to two separate Process
// calls, without major refactoring.)
//
// In the future, we could save these timestamps directly in a
// custom field, or pass the interval values through.
type ReconstructedTimestamps struct {
	RequestStart  time.Time
	RequestEnd    time.Time
	ResponseStart time.Time
	ResponseEnd   time.Time
}

func advanceTime(current time.Time, ms *float32) time.Time {
	if ms == nil || *ms <= 0.0 {
		return current
	}
	// convert ms to nanoseconds
	duration := time.Duration(int64(*ms * 1_000_000.0))
	return current.Add(duration)
}

func (t *ReconstructedTimestamps) reconstructFromHAR(entry *hl.CustomHAREntry) {
	currTime := entry.StartedDateTime
	if currTime.IsZero() {
		// Leave in all-zero state
		return
	}

	if entry.Timings == nil {
		// Mark them all with the timestamp we have
		t.RequestStart = entry.StartedDateTime
		t.RequestEnd = entry.StartedDateTime
		t.ResponseStart = entry.StartedDateTime
		t.ResponseEnd = entry.StartedDateTime
		return
	}

	// Pre-SYN activities
	currTime = advanceTime(currTime, entry.Timings.Blocked)
	currTime = advanceTime(currTime, entry.Timings.DNS)

	// SYN, handshake, and HTTP request
	t.RequestStart = currTime
	currTime = advanceTime(currTime, entry.Timings.Connect)
	currTime = advanceTime(currTime, entry.Timings.SSL)
	currTime = advanceTime(currTime, entry.Timings.Send)
	t.RequestEnd = currTime

	// Server processing time, before first byte of response
	currTime = advanceTime(currTime, entry.Timings.Wait)
	t.ResponseStart = currTime

	// Response arrives
	currTime = advanceTime(currTime, entry.Timings.Receive)
	t.ResponseEnd = currTime
}

// Returns the number of entries successfully collected from the given HAR log.
func parseFromHAR(col trace.Collector, log *hl.CustomHARLog) (int, sampled_err.Errors) {
	// Use the same UUID for all witnesses from the same HAR file.
	harUUID := uuid.New()

	successfulEntries := 0
	errs := sampled_err.Errors{SampleCount: 3}
	for i, entry := range log.Entries {
		entrySuccess := ProcessHAREntry(col, harUUID, i, entry, &errs)
		if entrySuccess {
			successfulEntries += 1
		}
	}
	return successfulEntries, errs
}

// Processes a single entry from a HAR file. The UUID identifies the HAR file,
// whereas the seqNum identifies the entry within the file.
//
// If any errors occur, the given Errors is updated, and false is returned.
// Otherwise, true is returned on success.
func ProcessHAREntry(col trace.Collector, harUUID uuid.UUID, seqNum int, entry hl.CustomHAREntry, errs *sampled_err.Errors) bool {
	entrySuccess := true

	var ts ReconstructedTimestamps
	ts.reconstructFromHAR(&entry)
	if entry.Request != nil {
		if req, err := convertRequest(entry.Request); err == nil {
			req.StreamID = harUUID
			req.Seq = seqNum
			col.Process(akinet.ParsedNetworkTraffic{
				ObservationTime: ts.RequestStart,
				FinalPacketTime: ts.RequestEnd,
				Content:         req,
			})
		} else {
			printer.Debugf("%s\n", errors.Wrapf(err, "failed to convert HAR request, index=%d", seqNum))
			entrySuccess = false
			errs.Add(err)
		}
	}
	if entry.Response != nil {
		if resp, err := convertResponse(entry.Response); err == nil {
			resp.StreamID = harUUID
			resp.Seq = seqNum
			col.Process(akinet.ParsedNetworkTraffic{
				ObservationTime: ts.ResponseStart,
				FinalPacketTime: ts.ResponseEnd,
				Content:         resp,
			})
		} else {
			printer.Debugf("%s\n", errors.Wrapf(err, "failed to convert HAR response, index=%d", seqNum))
			entrySuccess = false
			errs.Add(err)
		}
	}

	return entrySuccess
}

func convertRequest(harReq *har.Request) (akinet.HTTPRequest, error) {
	var req akinet.HTTPRequest
	err := req.FromHAR(harReq)
	return req, err
}

func convertResponse(harResp *har.Response) (akinet.HTTPResponse, error) {
	var resp akinet.HTTPResponse
	err := resp.FromHAR(harResp)
	return resp, err
}
