package cloud_client

import (
	"time"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"
)

// A loop that establishes a heartbeat connection to the cloud.
func (client *cloudClient) heartbeat() {
	frontClient := rest.NewFrontClient(client.host, client.clientID)
	for {
		if err := util.DaemonHeartbeat(frontClient); err != nil {
			printer.Warningf("Error sending heartbeat: %s", err)
		}
		time.Sleep(HEARTBEAT_INTERVAL)
	}
}
