package cloud_client

import "time"

// A loop that establishes a heartbeat connection to the cloud.
func (client *cloudClient) heartbeat() {
	for {
		// TODO
		time.Sleep(time.Second)
		print("lub ")
		time.Sleep(500 * time.Millisecond)
		println("dub")
	}
}
