package cloud_client

// An event that is handled by the main goroutine for the cloud client.
type Event interface {
	// Handles the event. Runs in the context of the main goroutine for the
	// given cloud client.
	handle(*cloudClient)
}
