package setversion

import (
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
)

type Args struct {
	// Required args
	ClientID akid.ClientID
	Domain   string

	ModelURI    akiuri.URI
	VersionName string
}
