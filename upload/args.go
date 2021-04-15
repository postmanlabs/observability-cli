package upload

import (
	"time"

	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
)

type Args struct {
	// Required args
	ClientID akid.ClientID
	Domain   string

	DestURI   akiuri.URI
	FilePaths []string

	// Optional args
	Append          bool
	IncludeTrackers bool
	UploadTimeout   time.Duration
	Plugins         []plugin.AkitaPlugin
}
