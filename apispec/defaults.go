package apispec

import "time"

// Specifies default values for command-line parameters.
//
// These really ought to live in apidump, but that package depends on libpcap.
// So we put these constants here to pulling in libpcap when importing these
// constants.
const (
	// Whether to send TCP and TLS reports to the back end.
	//
	// Invariant: if this is true, then so is DefaultParseTLSHandshakes.
	DefaultCollectTCPAndTLSReports = false

	// The name of the deployment.
	DefaultDeployment = "default"

	// The maximum witness size. Any witnesses larger than this are dropped.
	DefaultMaxWitnessSize_bytes = 30_000_000 // 30 MB

	// Whether to enable parsing of TLS handshakes.
	//
	// Invariant: if this is false, then so is DefaultCollectTCPAndTLSReports.
	DefaultParseTLSHandshakes = true

	// How many requests to capture per minute.
	DefaultRateLimit = 1000.0

	// How long to wait after starting up before printing packet-capture statistics.
	DefaultStatsLogDelay_seconds = 60

	// How often to upload client telemetry.
	DefaultTelemetryInterval_seconds = 5 * 60 // 5 minutes

	// How often to upload client telemetry.
	DefaultProcFSPollingInterval_seconds = 5 * 60 // 5 minutes

	// How often to rotate traces in the back end.
	DefaultTraceRotateInterval = time.Hour
)
