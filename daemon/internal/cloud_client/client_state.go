package cloud_client

import (
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/daemon"
)

// Logging state for a single service.
//
// Instances should only be accessed from within the main goroutine for the
// cloud client.
type serviceInfo struct {
	// The learning client for this service.
	learnClient rest.LearnClient

	// Contains an entry for each trace ID that is active or for which we are
	// collecting events.
	traces map[akid.LearnSessionID]*traceInfo

	// Contains names of clients waiting to hear about new traces, paired with
	// the channel on which to respond to the client.
	responseChannels []namedResponseChannel
}

type namedResponseChannel struct {
	clientName string
	channel    chan<- daemon.ActiveTraceDiff
}

func newNamedResponseChannel(clientName string, responseChannel chan<- daemon.ActiveTraceDiff) *namedResponseChannel {
	return &namedResponseChannel{
		clientName: clientName,
		channel:    responseChannel,
	}
}

func (client *cloudClient) newServiceInfo(serviceID akid.ServiceID) *serviceInfo {
	return &serviceInfo{
		learnClient:      client.newLearnClient(serviceID),
		traces:           map[akid.LearnSessionID]*traceInfo{},
		responseChannels: []namedResponseChannel{},
	}
}

// Returns a diff between the given set of traces and the set of traces known
// to be active. Also returns a list of traceInfo objects for newly actived
// traces.
func (info serviceInfo) getActiveTraceDiff(known_traces map[akid.LearnSessionID]struct{}) (daemon.ActiveTraceDiff, []*traceInfo) {
	activatedTraces := []daemon.LoggingOptions{}
	activatedInfo := []*traceInfo{}
	for traceID, traceInfo := range info.traces {
		// Ignore any inactive traces.
		if !traceInfo.active {
			continue
		}

		if _, ok := known_traces[traceID]; !ok {
			activatedTraces = append(activatedTraces, traceInfo.loggingOptions)
			activatedInfo = append(activatedInfo, traceInfo)
		}
	}

	deactivatedTraces := []akid.LearnSessionID{}
	for traceID := range known_traces {
		if traceInfo, ok := info.traces[traceID]; !ok || !traceInfo.active {
			deactivatedTraces = append(deactivatedTraces, traceID)
		}
	}

	return *daemon.NewActiveTraceDiff(activatedTraces, deactivatedTraces), activatedInfo
}

// Logging state for a single trace.
//
// Instances should only be accessed from within the main goroutine for the
// cloud client.
type traceInfo struct {
	// Whether the trace is active. If this is false, then the daemon is just
	// waiting for clients to finish sending their events.
	active bool

	// The names of the clients from which we are expecting to receive more
	// trace events. These are clients that are subscribed to the associated
	// service and have not signalled the end of their event stream for this
	// trace.
	clientNames map[string]struct{}

	// The trace's logging options.
	loggingOptions daemon.LoggingOptions

	// The channel on which to send trace events to the trace collector.
	traceEventChannel chan<- *TraceEvent

	// Channels to clients waiting to hear about the deactivation of the trace.
	deactivationChannels []chan<- struct{}
}

func newTraceInfo(loggingOptions daemon.LoggingOptions, traceEventChannel chan<- *TraceEvent) *traceInfo {
	return &traceInfo{
		active:               true,
		clientNames:          map[string]struct{}{},
		loggingOptions:       loggingOptions,
		traceEventChannel:    traceEventChannel,
		deactivationChannels: []chan<- struct{}{},
	}
}
