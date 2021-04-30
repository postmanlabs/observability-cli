package cloud_client

import "time"

// The number of events to buffer for any given trace before dropping incoming
// events.
const TRACE_BUFFER_SIZE = 10_000

// How long to wait between heartbeat requests.
const HEARTBEAT_INTERVAL = 30 * time.Second

// How long to wait after a failed long poll before trying again.
const LONG_POLL_INTERVAL = 5 * time.Second
