package har_loader

import (
	"encoding/json"
	"os"
	"time"

	"github.com/google/martian/v3/har"
	"github.com/pkg/errors"
)

// Custom HAR loader to bypass type differences between martian/v3/har and
// chrome output.

type CustomHAR struct {
	Log      *CustomHARLog      `json:"log"`
	AkitaExt har.AkitaExtension `json:"akita_ext"`
}

type CustomHARLog struct {
	Version string           `json:"version"`
	Creator *har.Creator     `json:"creator"`
	Entries []CustomHAREntry `json:"entries"`
	Comment string           `json:"comment"`
}

type CustomHAREntry struct {
	// Only include request and response since there are type conflicts in other
	// fields (e.g. timings).
	StartedDateTime time.Time     `json:"startedDateTime"`
	Request         *har.Request  `json:"request"`
	Response        *har.Response `json:"response"`
	Comment         string        `json:"comment"`
}

func LoadCustomHARFromFile(path string) (CustomHAR, error) {
	var harContent CustomHAR

	f, err := os.Open(path)
	if err != nil {
		return harContent, errors.Wrap(err, "failed to open HAR file")
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	if err := dec.Decode(&harContent); err != nil {
		return harContent, errors.Wrap(err, "failed to read HAR file")
	}
	if harContent.Log == nil {
		return harContent, errors.Errorf("HAR file does not contain log")
	}

	return harContent, nil
}
