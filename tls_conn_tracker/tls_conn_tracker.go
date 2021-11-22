package tls_conn_tracker

import (
	"net"
	"sync"
	"time"

	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
)

// Collects akinet.TLSClientHello and akinet.TLSServerHello messages and
// processes them into TLS-connection metadata. The downstream collector will
// receive one akinet.TLSConnectionMetadata per completed TLS handshake,
// summarizing what was observed about the handshake.
func NewCollector(next trace.Collector) trace.Collector {
	return &collector{
		collector: next,

		closed:            false,
		activeConnections: make(map[akid.ConnectionID]*connectionInfo),

		mutex: sync.Mutex{},
	}
}

// If a connection with a partial handshake has no activity after this period of
// time, the connection is assumed to have died, and its state is
// garbage-collected.
const connectionTimeout = 30 * time.Second

type collector struct {
	collector trace.Collector

	closed            bool
	activeConnections map[akid.ConnectionID]*connectionInfo

	// Protects this whole object.
	mutex sync.Mutex
}

var _ trace.Collector = (*collector)(nil)

// Caller must not hold c.mutex.
func (c *collector) Process(packet akinet.ParsedNetworkTraffic) error {
	switch tls := packet.Content.(type) {
	case akinet.TLSClientHello:
		c.mutex.Lock()
		defer c.mutex.Unlock()

		if c.closed {
			// XXX Warn? Error? None of the other collector implementations are very
			// careful with error-handling.
			return nil
		}

		info := c.ensureConnection(tls.ConnectionID, packet)
		if err := info.handshakeMetadata.AddClientHello(&tls); err != nil {
			return err
		}

		if info.handshakeMetadata.HandshakeComplete() {
			_, err := c.flushConnection(tls.ConnectionID)
			return err
		}

		return nil

	case akinet.TLSServerHello:
		c.mutex.Lock()
		defer c.mutex.Unlock()

		if c.closed {
			// XXX Warn? Error? None of the other collector implementations are very
			// careful with error-handling.
			return nil
		}

		info := c.ensureConnection(tls.ConnectionID, packet)
		if err := info.handshakeMetadata.AddServerHello(&tls); err != nil {
			return err
		}

		if info.handshakeMetadata.HandshakeComplete() {
			_, err := c.flushConnection(tls.ConnectionID)
			return err
		}

		return nil

	default:
		return c.collector.Process(packet)
	}
}

// Caller must not hold c.mutex.
func (c *collector) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.closed = true

	// Cancel the timeouts of all active connections and clear out those
	// connections.
	for _, info := range c.activeConnections {
		info.timeout.Stop()
	}
	c.activeConnections = map[akid.ConnectionID]*connectionInfo{}

	return c.collector.Close()
}

// Ensures the collector has an entry for the given connectionID and returns
// that entry. Caller must hold c.mutex.
func (c *collector) ensureConnection(id akid.ConnectionID, packet akinet.ParsedNetworkTraffic) *connectionInfo {
	info, exists := c.activeConnections[id]
	if !exists {
		// This is either a connection that we have timed out and flushed, or one
		// that the TCP-assembly layer thinks it hasn't seen before.
		info = c.addConnection(id, packet.SrcIP, packet.SrcPort, packet.DstIP, packet.DstPort, packet.ObservationTime)
	}
	return info
}

// Adds a new connection to the collector. Caller must hold c.mutex.
func (c *collector) addConnection(id akid.ConnectionID, srcIP net.IP, srcPort int, dstIP net.IP, dstPort int, observationTime time.Time) *connectionInfo {
	info := connectionInfo{
		srcIP:   srcIP,
		srcPort: srcPort,
		dstIP:   dstIP,
		dstPort: dstPort,

		firstObservationTime: observationTime,
		lastObservationTime:  observationTime,

		handshakeMetadata: akinet.TLSHandshakeMetadata{
			ConnectionID: id,
		},

		timeout: time.AfterFunc(connectionTimeout, func() {
			c.mutex.Lock()
			defer c.mutex.Unlock()
			delete(c.activeConnections, id)
		}),
	}

	c.activeConnections[id] = &info
	return &info
}

// Flushes a connection, if it exists, to the downstream collector. Caller must
// hold c.mutex. Returns true if the connection exists, false otherwise.
func (c *collector) flushConnection(id akid.ConnectionID) (bool, error) {
	info, exists := c.activeConnections[id]
	if !exists {
		return false, nil
	}

	err := c.collector.Process(akinet.ParsedNetworkTraffic{
		SrcIP:   info.srcIP,
		SrcPort: info.srcPort,
		DstIP:   info.dstIP,
		DstPort: info.dstPort,
		Content: info.handshakeMetadata,

		ObservationTime: info.firstObservationTime,
		FinalPacketTime: info.lastObservationTime,
	})

	delete(c.activeConnections, id)
	return true, err
}

// Internal representation of a TLS handshake.
type connectionInfo struct {
	srcIP   net.IP
	srcPort int
	dstIP   net.IP
	dstPort int

	firstObservationTime time.Time
	lastObservationTime  time.Time

	handshakeMetadata akinet.TLSHandshakeMetadata

	// Removes this connectionInfo from its parent collector, under the assumption
	// that the connection has died..
	timeout *time.Timer
}
