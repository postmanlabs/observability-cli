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
//
// NB 4/8/2021: I changed the Timings section in Martian to use floats, so
// I'm not sure what other type differences exist --MGG

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

// This contains all the fields used by Martian *and*
// by Chrome, as pointers so we can tell which ones
// are present.
type CustomTimings struct {
	Blocked         *float32 `json:"blocked"`
	DNS             *float32 `json:"dns"`
	SSL             *float32 `json:"ssl"`
	Connect         *float32 `json:"connect"`
	Send            *float32 `json:"send"`
	Wait            *float32 `json:"wait"`
	Receive         *float32 `json:"receive"`
	BlockedQueueing *float32 `json:"_blocked_queueing"`
}

type CustomHAREntry struct {
	// Only include fields we care about
	StartedDateTime time.Time      `json:"startedDateTime"`
	Request         *har.Request   `json:"request"`
	Response        *har.Response  `json:"response"`
	Comment         string         `json:"comment"`
	Timings         *CustomTimings `json:"timings"`
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
