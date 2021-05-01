package cloud_client

import (
	"time"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/util"
)

// Occurs when the daemon should send a heartbeat to the cloud.
type heartbeatEvent struct{}

func newHeartbeatEvent() heartbeatEvent {
	return heartbeatEvent{}
}

func (event heartbeatEvent) handle(client *cloudClient) {
	printer.Debugf("Heartbeat...\n")
	frontClient := client.frontClient
	daemonName := client.daemonName

	go func() {
		if err := util.DaemonHeartbeat(frontClient, daemonName); err != nil {
			printer.Warningf("Error sending heartbeat: %s\n", err)
		}
		time.Sleep(HEARTBEAT_INTERVAL)

		client.eventChannel <- event
	}()
}
