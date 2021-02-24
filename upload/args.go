package upload

import (
	"time"

	"github.com/akitasoftware/akita-libs/akid"
)

type Args struct {
	// Required args
	ClientID akid.ClientID
	Domain   string

	Service  string
	SpecPath string

	// Optional args
	SpecName      string
	UploadTimeout time.Duration
}
