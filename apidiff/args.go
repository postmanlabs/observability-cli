package apidiff

import (
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
)

type Args struct {
	ClientID akid.ClientID
	Domain   string

	BaseSpecURI akiuri.URI
	NewSpecURI  akiuri.URI

	// If unset, defaults to interactive mode.
	// Use '-' for stdout.
	Out string
}
