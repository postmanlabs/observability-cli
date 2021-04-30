package cloud_client

import (
	"time"

	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/daemon"
	"github.com/akitasoftware/akita-libs/sampled_err"
	"github.com/google/uuid"
)

type cloudClient struct {
	host     string
	clientID akid.ClientID
	plugins  []plugin.AkitaPlugin

	// Tracking for each registered service.
	serviceInfoByID map[akid.ServiceID]*serviceInfo

	// The main stream of events.
	eventChannel chan Event
}

func newCloudClient(host string, clientID akid.ClientID, plugins []plugin.AkitaPlugin) *cloudClient {
	return &cloudClient{
		host:            host,
		clientID:        clientID,
		plugins:         plugins,
		serviceInfoByID: make(map[akid.ServiceID]*serviceInfo),
		eventChannel:    make(chan Event),
	}
}

// Logging state for a single service.
type serviceInfo struct {
	// The learning client for this service.
	LearnClient rest.LearnClient

	// Only populated when logging is active for the service.
	LoggingOptions *daemon.LoggingOptions

	// Contains channels to clients awaiting a status change.
	ResponseChannels []chan<- ClientLoggingState

	// Contains an entry for each trace ID for which we are collecting events.
	Traces map[akid.LearnSessionID]*traceInfo
}

func (client *cloudClient) newServiceInfo(serviceID akid.ServiceID) *serviceInfo {
	return &serviceInfo{
		LearnClient:      client.NewLearnClient(serviceID),
		LoggingOptions:   nil,
		ResponseChannels: []chan<- ClientLoggingState{},
		Traces:           make(map[akid.LearnSessionID]*traceInfo),
	}
}

// Registers the service with the daemon if needed. Upon registration, a
// goroutine is started for long-polling the cloud for the service's state.
func (client *cloudClient) ensureServiceRegistered(serviceID akid.ServiceID) *serviceInfo {
	if serviceInfo, ok := client.serviceInfoByID[serviceID]; ok {
		return serviceInfo
	}

	serviceInfo := client.newServiceInfo(serviceID)
	client.serviceInfoByID[serviceID] = serviceInfo
	client.longPollService(serviceID)
	return serviceInfo
}

// Logging state for a single trace.
type traceInfo struct {
	// The number of clients from which we are receiving trace events.
	numClients int

	// The channel on which to send trace events to the trace collector.
	traceEventChannel chan<- *TraceEvent
}

// An event that is handled by the main goroutine for the cloud client.
type Event interface {
	// Handles the event. Runs in the context of the main goroutine for the
	// given cloud client.
	handle(*cloudClient)
}

// Instantiates a cloud client and starts its main goroutine. Returns a
// channel on which requests to the client can be made.
func Run(host string, clientID akid.ClientID, plugins []plugin.AkitaPlugin) chan<- Event {
	client := newCloudClient(host, clientID, plugins)

	// Start the main goroutine for the cloud client.
	//
	// Accesses to anything inside client.serviceInfoByID must be done in this
	// goroutine.
	go func() {
		// Start the heartbeat connection to the cloud.
		go client.heartbeat()

		for event := range client.eventChannel {
			event.handle(client)
		}
	}()

	return client.eventChannel
}

func (client *cloudClient) NewLearnClient(serviceID akid.ServiceID) rest.LearnClient {
	return rest.NewLearnClient(client.host, client.clientID, serviceID)
}

// Determines whether trace events are being logged for the given service.
func (client *cloudClient) isCurrentlyLogging(serviceID akid.ServiceID) bool {
	return client.serviceInfoByID[serviceID].LoggingOptions != nil
}

// Starts a goroutine for long-polling the cloud for updates on the logging
// status for the given service.
func (client *cloudClient) longPollService(serviceID akid.ServiceID) {
	currentlyLogging := client.isCurrentlyLogging(serviceID)
	learnClient := client.serviceInfoByID[serviceID].LearnClient
	go func() {
		for {
			loggingState, err := util.LongPollServiceLoggingStatus(learnClient, serviceID, currentlyLogging)
			if err != nil {
				printer.Debugf("Error while polling %s: %v", akid.String(serviceID), err)
				time.Sleep(LONG_POLL_INTERVAL)
				continue
			}

			// Enqueue a LoggingStartStopEvent for the main goroutine to handle.
			client.eventChannel <- NewLoggingStartStopEvent(serviceID, loggingState.LoggingOptions)
			return
		}
	}()
}

// Ensures we have a goroutine for collecting trace events and sending them to
// the cloud. Assumes the given service ID has been registered. Returns nil if
// there is no event collector for the given trace and the daemon's state
// indicates that logging is stopped.
func (client *cloudClient) ensureTraceEventCollector(serviceID akid.ServiceID, traceID akid.LearnSessionID) *traceInfo {
	serviceInfo := client.serviceInfoByID[serviceID]
	if traceInfo, ok := serviceInfo.Traces[traceID]; ok {
		// Already collecting trace events.
		return traceInfo
	}

	if serviceInfo.LoggingOptions == nil {
		return nil
	}

	// We've discovered a new trace. Start a collector goroutine.
	learnClient := serviceInfo.LearnClient
	filterThirdPartyTrackers := serviceInfo.LoggingOptions.FilterThirdPartyTrackers
	traceEventChannel := make(chan *TraceEvent, TRACE_BUFFER_SIZE)
	go func() {
		// Create the collector.
		collector := trace.NewBackendCollector(serviceID, traceID, learnClient, api_schema.Inbound, client.plugins)
		if filterThirdPartyTrackers {
			collector = trace.New3PTrackerFilterCollector(collector)
		}

		// Create a new stream ID for the trace events we are about to process.
		streamID := uuid.New()

		successfulEntries := 0
		sampledErrs := sampled_err.Errors{SampleCount: 3}
		for seqNum := 0; true; seqNum++ {
			// Get the next trace event.
			traceEvent, more := <-traceEventChannel
			if !more {
				break
			}

			// Pass the trace event to the collector.
			if entrySuccess := apispec.ProcessHAREntry(collector, streamID, seqNum, *traceEvent, &sampledErrs); entrySuccess {
				successfulEntries += 1
			}
		}

		// TODO: Log successfulEntries and sampledErrs.
	}()

	// Register the newly discovered trace.
	traceInfo := &traceInfo{
		numClients:        0,
		traceEventChannel: traceEventChannel,
	}
	serviceInfo.Traces[traceID] = traceInfo
	return traceInfo
}
