package learn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"path"
	"strconv"

	"github.com/OneOfOne/xxhash"
	"github.com/google/martian/v3/har"
	"github.com/pkg/errors"

	col "github.com/akitasoftware/akita-cli/pcap"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/version"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
	akihttp "github.com/akitasoftware/akita-libs/akinet/http"
	"github.com/akitasoftware/akita-libs/spec_util"
)

// Responsible for processing witnesses collected by the broker.
type WitnessProcessor interface {
	ProcessWitness(*witnessResult) error

	// Implementations must complete all witnesses sent to ProcessWitness before
	// returning.
	Close()
}

type HAROptions struct {
	SampleRate float64
	OutDir     string
}

// Starts collecting witnesses and blocks until stop is closed.
// Closes proc upon return.
func CollectWitnesses(stop <-chan struct{}, intf, bpfFilter string, proc WitnessProcessor, harOpts *HAROptions) error {
	facts := []akinet.TCPParserFactory{
		akihttp.NewHTTPRequestParserFactory(),
		akihttp.NewHTTPResponseParserFactory(),
	}
	parser := col.NewNetworkTrafficParser()
	parsedChan, err := parser.ParseFromInterface(intf, bpfFilter, stop, facts...)
	if err != nil {
		return errors.Wrap(err, "couldn't start parsing from interface")
	}

	var harDone chan struct{}
	if harOpts != nil {
		harDone = make(chan struct{})

		// Tee parsedChan so we can generate a HAR file.
		c1, c2 := akinet.Tee(parsedChan)
		parsedChan = c1
		go func() {
			defer close(harDone)
			createHARFile(c2, intf, harOpts)
		}()
	}

	err = CollectWitnessesFromChannel(parsedChan, proc)

	// Wait for the HAR file to be done.
	if harDone != nil {
		printer.Infof("Waiting for HAR file to finish writing...\n")
		<-harDone
	}

	return err
}

func CollectWitnessesFromChannel(parsedChan <-chan akinet.ParsedNetworkTraffic, proc WitnessProcessor) error {
	defer proc.Close()

	witnessChan := startLearning(parsedChan)
	for r := range witnessChan {
		// Skip witnesses of CLI to backend API calls that were accidentally
		// captured.
		if spec_util.ContainsCLITraffic(r.witness) {
			printer.Debugf("Skipping witness containing Akita API call\n")
			continue
		}

		if err := proc.ProcessWitness(r); err != nil {
			return errors.Wrap(err, "couldn't write witness")
		}
	}
	return nil
}

func containsCLITraffic(c akinet.ParsedNetworkContent) bool {
	var header http.Header
	switch tc := c.(type) {
	case akinet.HTTPRequest:
		header = tc.Header
	case akinet.HTTPResponse:
		header = tc.Header
	}

	for _, k := range []string{spec_util.XAkitaCLIGitVersion, spec_util.XAkitaRequestID, spec_util.XAkitaDogfood} {
		if header.Get(k) != "" {
			return true
		}
	}
	return false
}

func createHARFile(in <-chan akinet.ParsedNetworkTraffic, interfaceName string, opts *HAROptions) {
	threshold := float64(math.MaxUint32) * opts.SampleRate
	includeSample := func(k string) bool {
		h := xxhash.New32()
		h.WriteString(k)
		return float64(h.Sum32()) < threshold
	}

	l := har.NewLogger()
	for t := range in {
		if containsCLITraffic(t.Content) {
			continue
		}

		switch c := t.Content.(type) {
		case akinet.HTTPRequest:
			id := toWitnessID(c.StreamID, c.Seq)

			// Sample based on pair key so the request and response are either both
			// selected or both excluded.
			if includeSample(akid.String(id)) {
				req := &http.Request{
					Method:        c.Method,
					URL:           c.URL,
					Proto:         fmt.Sprintf("HTTP/%d.%d", c.ProtoMajor, c.ProtoMinor),
					ProtoMajor:    c.ProtoMajor,
					ProtoMinor:    c.ProtoMinor,
					Header:        c.Header,
					Body:          ioutil.NopCloser(bytes.NewReader(c.Body)),
					ContentLength: int64(len(c.Body)),
					Host:          c.Host,
				}
				for _, cookie := range c.Cookies {
					req.AddCookie(cookie)
				}
				l.RecordRequest(akid.String(id), req)
			}
		case akinet.HTTPResponse:
			id := toWitnessID(c.StreamID, c.Seq)

			// Sample based on pair key so the request and response are either both
			// selected or both excluded.
			if includeSample(akid.String(id)) {
				resp := &http.Response{
					Status:        strconv.Itoa(c.StatusCode) + " " + http.StatusText(c.StatusCode),
					StatusCode:    c.StatusCode,
					Proto:         fmt.Sprintf("HTTP/%d.%d", c.ProtoMajor, c.ProtoMinor),
					ProtoMajor:    c.ProtoMajor,
					ProtoMinor:    c.ProtoMinor,
					Header:        c.Header,
					Body:          ioutil.NopCloser(bytes.NewReader(c.Body)),
					ContentLength: int64(len(c.Body)),
				}
				l.RecordResponse(akid.String(id), resp)
			}
		}
	}

	harContent := l.ExportAndReset()

	if log := harContent.Log; log != nil {
		if len(log.Entries) == 0 {
			// No need to write an empty file.
			return
		}

		// Customize the creator info.
		log.Creator = &har.Creator{
			Name:    "Akita SuperLearn (https://akitasoftware.com)",
			Version: version.CLIDisplayString(),
		}
	} else {
		// No need to write an empty file.
		return
	}

	harBytes, err := json.Marshal(harContent)
	if err != nil {
		printer.Errorf("Failed to marshal HAR to JSON: %v\n", err)
	}

	outPath := path.Join(opts.OutDir, fmt.Sprintf("akita_%s.har", interfaceName))
	if err := ioutil.WriteFile(outPath, harBytes, 0644); err != nil {
		printer.Errorf("Failed to write HAR file: %v\n", err)
	}
}
