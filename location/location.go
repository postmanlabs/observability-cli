package location

import (
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-libs/akiuri"
)

// Implements pflag.Value interface.
// Exactly one of LocalPath or AkitaURI is set.
type Location struct {
	LocalPath *string
	AkitaURI  *akiuri.URI
}

func (l Location) String() string {
	if l.LocalPath != nil {
		return *l.LocalPath
	} else if l.AkitaURI != nil {
		return l.AkitaURI.String()
	}
	return ""
}

func (l *Location) Set(raw string) error {
	if len(raw) == 0 {
		return errors.Errorf("location cannot be empty")
	}

	if u, err := akiuri.Parse(raw); err == nil {
		l.AkitaURI = &u
	} else {
		l.LocalPath = &raw
	}
	return nil
}

func (Location) Type() string {
	return "location"
}

func (l Location) IsSet() bool {
	return l.LocalPath != nil || l.AkitaURI != nil
}
