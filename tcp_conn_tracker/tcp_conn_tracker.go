package tcp_conn_tracker

import (
	"net"
	"sync"
	"time"

	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/sampled_err"
)

// Collects akinet.TCPPacketMetadata and processes them into summaries. The
// downstream collector will receive one akinet.TCPConnectionMetadata per TCP
// connection, summarizing what was observed about the connection.
func NewCollector(next trace.Collector) trace.Collector {
	return &collector{
		collector: next,
	}
}

// If a connection has no activity after this period of time, the connection's
// state is flushed to the downstream collector and is removed from the set of
// active connections.
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
	switch tcp := packet.Content.(type) {
	case akinet.TCPPacketMetadata:
		c.mutex.Lock()
		defer c.mutex.Unlock()

		if c.closed {
			// XXX Warn? Error? None of the other collector implementations are very
			// careful with error-handling.
			return nil
		}

		info, exists := c.activeConnections[tcp.ConnectionID]
		if !exists {
			c.addConnection(packet.SrcIP, packet.SrcPort, packet.DstIP, packet.DstPort, packet.ObservationTime, tcp)
			return nil
		}

		info.augmentWith(packet, &tcp)
	}

	return c.collector.Process(packet)
}

// Caller must not hold c.mutex.
func (c *collector) Close() error {
	err := c.close()
	if closeErr := c.collector.Close(); closeErr != nil {
		err.Add(closeErr)
	}

	if err.TotalCount > 0 {
		return err
	}
	return nil
}

// Caller must not hold c.mutex.
func (c *collector) close() sampled_err.Errors {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.closed = true

	err := sampled_err.Errors{SampleCount: 5}

	// Flush active connections to the downstream collector while cancelling their
	// timeouts.
	for _, info := range c.activeConnections {
		info.timeout.Stop()

		processErr := c.collector.Process(akinet.ParsedNetworkTraffic{
			SrcIP:   info.srcIP,
			SrcPort: info.srcPort,
			DstIP:   info.dstIP,
			DstPort: info.dstPort,
			Content: info.tcpMetadata,
		})

		if processErr != nil {
			err.Add(processErr)
		}
	}
	c.activeConnections = map[akid.ConnectionID]*connectionInfo{}

	return err
}

// Flushes a connection if it exists. Caller must not hold c.mutex. Returns true
// if the connection exists, false otherwise.
func (c *collector) flushConnection(connectionID akid.ConnectionID) (bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	info, exists := c.activeConnections[connectionID]
	if !exists {
		return false, nil
	}

	err := c.collector.Process(akinet.ParsedNetworkTraffic{
		SrcIP:   info.srcIP,
		SrcPort: info.srcPort,
		DstIP:   info.dstIP,
		DstPort: info.dstPort,
		Content: info.tcpMetadata,
	})

	delete(c.activeConnections, connectionID)
	return true, err
}

// Adds a new connection to the collector. Caller must hold c.mutex.
func (c *collector) addConnection(srcIP net.IP, srcPort int, dstIP net.IP, dstPort int, observationTime time.Time, metadata akinet.TCPPacketMetadata) {
	info := connectionInfo{
		srcIP:   srcIP,
		srcPort: srcPort,
		dstIP:   dstIP,
		dstPort: dstPort,

		firstObservationTime: observationTime,
		lastObservationTime:  observationTime,

		tcpMetadata: akinet.TCPConnectionMetadata{
			ConnectionID: metadata.ConnectionID,
			Direction:    akinet.UnknownTCPConnectionDirection,
			EndState:     akinet.StillOpen,
		},

		timeout: time.AfterFunc(connectionTimeout, func() {
			c.flushConnection(metadata.ConnectionID)
		}),
	}

	c.activeConnections[metadata.ConnectionID] = &info
}

// Internal representation of an observed TCP connection.
type connectionInfo struct {
	srcIP   net.IP
	srcPort int
	dstIP   net.IP
	dstPort int

	firstObservationTime time.Time
	lastObservationTime  time.Time

	tcpMetadata akinet.TCPConnectionMetadata

	// Flushes this connectionInfo to the downstream collector and removes this
	// connectionInfo from its parent collector.
	timeout *time.Timer
}

func (info *connectionInfo) augmentWith(packet akinet.ParsedNetworkTraffic, metadata *akinet.TCPPacketMetadata) {
	if packet.ObservationTime.Before(info.firstObservationTime) {
		info.firstObservationTime = packet.ObservationTime
	}

	if packet.ObservationTime.After(info.lastObservationTime) {
		info.lastObservationTime = packet.ObservationTime
	}

	// Try to infer the connection's direction, if not already inferred.
	if info.tcpMetadata.Direction == akinet.UnknownTCPConnectionDirection && metadata.SYN {
		if metadata.ACK {
			// SYN-ACK packet. Packet's destination connected to packet's source.
			if info.srcIP.Equal(packet.SrcIP) && info.srcPort == packet.SrcPort {
				info.tcpMetadata.Direction = akinet.DestToSource
			} else {
				info.tcpMetadata.Direction = akinet.SourceToDest
			}
		} else {
			// SYN packet. Packet's source connected to packet's destination.
			if info.srcIP.Equal(packet.SrcIP) && info.srcPort == packet.SrcPort {
				info.tcpMetadata.Direction = akinet.SourceToDest
			} else {
				info.tcpMetadata.Direction = akinet.DestToSource
			}
		}
	}

	// Set the closed state if the FIN flag is set, but don't let it override a
	// previously observed connection reset.
	if metadata.FIN && info.tcpMetadata.EndState == akinet.StillOpen {
		info.tcpMetadata.EndState = akinet.ConnectionClosed
	}

	if metadata.RST {
		info.tcpMetadata.EndState = akinet.ConnectionReset
	}

	info.timeout.Reset(connectionTimeout)
}
