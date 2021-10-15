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

		closed:            false,
		activeConnections: make(map[akid.ConnectionID]*connectionInfo),

		mutex: sync.Mutex{},
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
			// This is either a connection that we have timed out and flushed, or one
			// that the TCP-reassembly layer thinks it hasn't seen before. However,
			// the TCP-reassembly layer gets confused by the final "ACK" sent after a
			// connection is closed, and thinks it is part of a new connection. We
			// therefore ignore any packets having no payload and just the ACK flag
			// set. This trades a small amount of accuracy in connection-observation
			// times for greater accuracy in the set of connections observed.
			if tcp.PayloadLength_bytes == 0 && tcp.ACK && !(tcp.FIN || tcp.RST || tcp.SYN) {
				return nil
			}

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

			ObservationTime: info.firstObservationTime,
			FinalPacketTime: info.lastObservationTime,
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

		ObservationTime: info.firstObservationTime,
		FinalPacketTime: info.lastObservationTime,
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
			Initiator:    akinet.UnknownTCPConnectionInitiator,
			EndState:     akinet.ConnectionOpen,
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

	// Try to infer the connection's initiator, if not already inferred.
	if info.tcpMetadata.Initiator == akinet.UnknownTCPConnectionInitiator && metadata.SYN {
		if metadata.ACK {
			// SYN-ACK packet. Packet's destination connected to packet's source.
			if info.srcIP.Equal(packet.SrcIP) && info.srcPort == packet.SrcPort {
				info.tcpMetadata.Initiator = akinet.DestInitiator
			} else {
				info.tcpMetadata.Initiator = akinet.SourceInitiator
			}
		} else {
			// SYN packet. Packet's source connected to packet's destination.
			if info.srcIP.Equal(packet.SrcIP) && info.srcPort == packet.SrcPort {
				info.tcpMetadata.Initiator = akinet.SourceInitiator
			} else {
				info.tcpMetadata.Initiator = akinet.DestInitiator
			}
		}
	}

	// Set the closed state if the FIN flag is set, but don't let it override a
	// previously observed connection reset.
	if metadata.FIN && info.tcpMetadata.EndState == akinet.ConnectionOpen {
		info.tcpMetadata.EndState = akinet.ConnectionClosed
	}

	if metadata.RST {
		info.tcpMetadata.EndState = akinet.ConnectionReset
	}

	info.timeout.Reset(connectionTimeout)
}
