package akiflag

import "github.com/akitasoftware/akita-libs/akid"

// A set of variables holding the values of global flags exposed by the root
// command. This allows us to share those values with subcommands without
// creating an import loop.

var Domain string

// Returns a global ClientID, generated once per CLI instance.
func GetClientID() akid.ClientID {
	if clientID == nil {
		newID := akid.GenerateClientID()
		clientID = &newID
	}
	return *clientID
}

var clientID *akid.ClientID
