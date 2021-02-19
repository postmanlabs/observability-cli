package trace

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path"

	"github.com/google/martian/v3/har"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/learn"
	"github.com/akitasoftware/akita-cli/version"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
)

type HARCollector struct {
	logger        *har.Logger
	interfaceName string
	outDir        string

	isOutbound bool
	tags       map[string]string
}

func NewHARCollector(interfaceName, outDir string, isOutbound bool, tags map[string]string) *HARCollector {
	return &HARCollector{
		logger:        har.NewLogger(),
		interfaceName: interfaceName,
		outDir:        outDir,
		isOutbound:    isOutbound,
		tags:          tags,
	}
}

func (h *HARCollector) Process(t akinet.ParsedNetworkTraffic) error {
	switch c := t.Content.(type) {
	case akinet.HTTPRequest:
		id := learn.ToWitnessID(c.StreamID, c.Seq)
		h.logger.RecordRequest(akid.String(id), c.ToStdRequest())
	case akinet.HTTPResponse:
		id := learn.ToWitnessID(c.StreamID, c.Seq)
		h.logger.RecordResponse(akid.String(id), c.ToStdResponse())
	}
	return nil
}

// TODO: output HAR files periodically instead of buffering everything in
// memory.
func (h *HARCollector) Close() error {
	harContent := h.logger.ExportAndReset()

	// Record AkitaExtension
	harContent.AkitaExt = har.AkitaExtension{
		Outbound: h.isOutbound,
		Tags:     h.tags,
	}

	if log := harContent.Log; log != nil {
		if len(log.Entries) == 0 {
			// No need to write an empty file.
			return nil
		}

		// Customize the creator info.
		log.Creator = &har.Creator{
			Name:    "Akita SuperLearn (https://akitasoftware.com)",
			Version: version.CLIDisplayString(),
		}
	} else {
		// No need to write an empty file.
		return nil
	}

	harBytes, err := json.Marshal(harContent)
	if err != nil {
		return errors.Wrap(err, "failed to marshal HAR to JSON")
	}

	outPath := path.Join(h.outDir, fmt.Sprintf("akita_%s.har", h.interfaceName))
	if err := ioutil.WriteFile(outPath, harBytes, 0644); err != nil {
		return errors.Wrap(err, "failed to write HAR file")
	}
	return nil
}
