package telemetry

import (
	"sync"

	"github.com/akitasoftware/akita-libs/akid"
)

// Returns a global ClientID, generated once per CLI instance.
func GetClientID() akid.ClientID {
	clientOnce.Do(func() {
		clientID = akid.GenerateClientID()
	})
	return clientID
}

var clientID akid.ClientID
var clientOnce sync.Once
