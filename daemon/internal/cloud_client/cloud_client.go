package cloud_client

import (
	"fmt"

	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/daemon"
	"github.com/akitasoftware/akita-libs/sampled_err"
	"github.com/google/uuid"
)

type cloudClient struct {
	daemonName  string
	host        string
	clientID    akid.ClientID
	plugins     []plugin.AkitaPlugin
	frontClient rest.FrontClient

	// Tracking for each registered service.
	serviceInfoByID map[akid.ServiceID]*serviceInfo

	// The main stream of events.
	eventChannel chan Event
}

func newCloudClient(daemonName, host string, clientID akid.ClientID, plugins []plugin.AkitaPlugin) *cloudClient {
	return &cloudClient{
		daemonName:      daemonName,
		host:            host,
		clientID:        clientID,
		plugins:         plugins,
		frontClient:     rest.NewFrontClient(host, clientID),
		serviceInfoByID: make(map[akid.ServiceID]*serviceInfo),
		eventChannel:    make(chan Event, MAIN_GOROUTINE_BUFFER_SIZE),
	}
}

// Instantiates a cloud client and starts its main goroutine. Returns a
// channel on which requests to the client can be made.
func Run(daemonName, host string, clientID akid.ClientID, plugins []plugin.AkitaPlugin) chan<- Event {
	client := newCloudClient(daemonName, host, clientID, plugins)

	// Start the main goroutine for the cloud client.
	//
	// Accesses to anything inside client.serviceInfoByID must be done in this
	// goroutine.
	go func() {
		for event := range client.eventChannel {
			event.handle(client)
		}

		printer.Debugf("Main worker has shut down")
	}()

	// Start the heartbeat connection to the cloud.
	client.eventChannel <- newHeartbeatEvent()

	return client.eventChannel
}

func (client *cloudClient) newLearnClient(serviceID akid.ServiceID) rest.LearnClient {
	return rest.NewLearnClient(client.host, client.clientID, serviceID)
}

// A helper for obtaining the service and trace info corresponding to a given
// service and trace.
//
// This should only be called from within the main goroutine for the cloud
// client.
func (client *cloudClient) getInfo(serviceID akid.ServiceID, traceID akid.LearnSessionID) (*serviceInfo, *traceInfo) {
	serviceInfo, ok := client.serviceInfoByID[serviceID]
	if !ok {
		return nil, nil
	}

	traceInfo, ok := serviceInfo.traces[traceID]
	if !ok {
		return serviceInfo, nil
	}

	return serviceInfo, traceInfo
}

// Registers the service with the daemon if needed. Upon registration, a
// longPollServiceEvent is scheduled for the service.
//
// This should only be called from within the main goroutine for the cloud
// client.
func (client *cloudClient) ensureServiceRegistered(serviceID akid.ServiceID) *serviceInfo {
	if serviceInfo, ok := client.serviceInfoByID[serviceID]; ok {
		// Service already registered.
		return serviceInfo
	}

	// Register the new service and schedule a longPollServiceEvent.
	serviceInfo := client.newServiceInfo(serviceID)
	client.serviceInfoByID[serviceID] = serviceInfo
	client.eventChannel <- newLongPollServiceEvent(serviceID)

	return serviceInfo
}

// Determines which traces are being collected for the given service.
//
// This should only be called from within the main goroutine for the cloud
// client.
func (client *cloudClient) getCurrentTraces(serviceID akid.ServiceID) []akid.LearnSessionID {
	result := []akid.LearnSessionID{}
	for traceID := range client.serviceInfoByID[serviceID].traces {
		result = append(result, traceID)
	}
	return result
}

// Starts a goroutine for collecting trace events and sending them to the
// cloud. Assumes the given service ID has been registered.
//
// This should only be called from within the main goroutine for the cloud
// client.
func (client *cloudClient) startTraceEventCollector(serviceID akid.ServiceID, loggingOptions daemon.LoggingOptions) {
	serviceInfo, traceInfo := client.getInfo(serviceID, loggingOptions.TraceID)
	if serviceInfo == nil {
		printer.Warningf("Got a new trace from the cloud for an unregistered service: %q\n", akid.String(serviceID))
		return
	}

	if traceInfo != nil {
		if traceInfo.active {
			printer.Debugf("Got an allegedly new trace from the cloud, but already collecting events for that trace: %q\n", akid.String(loggingOptions.TraceID))
		}

		// Reactivate the trace and update its logging options.
		traceInfo.active = true
		traceInfo.loggingOptions = loggingOptions
		return
	}

	// Start a collector goroutine.
	traceEventChannel := make(chan *TraceEvent, TRACE_BUFFER_SIZE)
	go collectTraces(traceEventChannel, serviceInfo.learnClient, serviceID, loggingOptions, client.plugins)

	// Register the newly discovered trace.
	serviceInfo.traces[loggingOptions.TraceID] = newTraceInfo(loggingOptions, traceEventChannel)
}

func collectTraces(traceEventChannel <-chan *TraceEvent, learnClient rest.LearnClient, serviceID akid.ServiceID, loggingOptions daemon.LoggingOptions, plugins []plugin.AkitaPlugin) {
	// Create the collector.
	collector := trace.NewBackendCollector(serviceID, loggingOptions.TraceID, learnClient, api_schema.Inbound, plugins, nil)
	if loggingOptions.FilterThirdPartyTrackers {
		collector = trace.New3PTrackerFilterCollector(collector)
	}
	defer collector.Close()

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

	// Log successfulEntries and sampledErrs.
	printer.Infof("Collected %d entries for trace %q\n", successfulEntries, loggingOptions.TraceID)
	if sampledErrs.TotalCount > 0 {
		sampledErrsStr := ""
		for _, e := range sampledErrs.Samples {
			sampledErrsStr = fmt.Sprintf("%s\t- %s\n", sampledErrsStr, e)
		}
		printer.Stderr.Warningf(`Encountered errors with %d entries for trace %q.\n
				Akita will ignore entries with errors and proceed with the %d entries successfully processed.\n
				Sample errors:\n
				%s`, sampledErrs.TotalCount, loggingOptions.TraceID, successfulEntries, sampledErrsStr)
	}
}

// This should only be called from within the main goroutine for the cloud
// client.
func (client *cloudClient) unregisterTrace(serviceID akid.ServiceID, traceID akid.LearnSessionID) {
	serviceInfo, traceInfo := client.getInfo(serviceID, traceID)
	if serviceInfo == nil {
		printer.Debugf("Tried to unregister a trace from an unknown service %q\n", akid.String(serviceID))
		return
	}

	if traceInfo == nil {
		printer.Debugf("Tried to unregister an unknown trace %q\n", akid.String(traceID))
		return
	}

	if traceInfo.active {
		printer.Debugf("Tried to unregister an active trace %q; ignoring\n", akid.String(traceID))
		return
	}

	if len(traceInfo.clientNames) > 0 {
		printer.Debugf("Tried to unregister trace %q for which clients are still sending events; ignoring\n", akid.String(traceID))
		return
	}

	delete(serviceInfo.traces, traceID)
}
