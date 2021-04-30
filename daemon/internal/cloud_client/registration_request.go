package cloud_client

import (
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/daemon"
)

// A request for registering a client to the daemon.
type RegistrationRequest struct {
	// The service with which the client is associated.
	ServiceID akid.ServiceID

	// Indicates whether the client is currently logging trace events.
	ClientLoggingState bool

	// The channel on which to send the response to this request.
	ResponseChannel chan<- ClientLoggingState
}

// A response to RegistrationRequest.
type ClientLoggingState struct {
	// The updated logging state.
	LoggingState bool `json:"logging"`

	// Specifies how trace events should be logged by the client. Only populated
	// when LoggingState is true.
	LoggingOptions *daemon.LoggingOptions `json:"loggingOptions,omitempty"`
}

func NewClientLoggingState(LoggingOptions *daemon.LoggingOptions) ClientLoggingState {
	return ClientLoggingState{
		LoggingState:   LoggingOptions != nil,
		LoggingOptions: LoggingOptions,
	}
}

func (req RegistrationRequest) handle(client *cloudClient) {
	// Register the service if it's not already registered.
	serviceInfo := client.ensureServiceRegistered(req.ServiceID)

	// If the logging state is different from what the client is reporting, then
	// send back what we already have.
	currentlyLogging := serviceInfo.LoggingOptions != nil
	if currentlyLogging != req.ClientLoggingState {
		defer close(req.ResponseChannel)
		req.ResponseChannel <- NewClientLoggingState(serviceInfo.LoggingOptions)
		return
	}

	// Register the client for the eventual response.
	serviceInfo.ResponseChannels = append(serviceInfo.ResponseChannels, req.ResponseChannel)
}
