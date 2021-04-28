package trace

import (
	"sync"

	"github.com/akitasoftware/akita-cli/pcap"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// We produce a set of packet counters indexed by interface and
// port number (*either* source or destination.)
type PacketCounters struct {
	// Flow
	Interface string
	SrcPort   int
	DstPort   int

	// Number of events
	TCPPackets    int
	HTTPRequests  int
	HTTPResponses int
	Unparsed      int
}

func (c *PacketCounters) Add(d PacketCounters) {
	c.TCPPackets += d.TCPPackets
	c.HTTPRequests += d.HTTPRequests
	c.HTTPResponses += d.HTTPResponses
	c.Unparsed += d.Unparsed
}

// A consumer accepts incremental updates in the form
// of PacketCounters.
type PacketCountConsumer interface {
	// Add an additional measurement to the current count
	Update(delta PacketCounters)
}

// Discard the count
type PacketCountDiscard struct {
}

func (d *PacketCountDiscard) Update(_ PacketCounters) {
}

// A consumer that sums the count by (interface, port) pairs.
// In the future, this could put counters on a pipe and do the increments
// in a separate goroutine, but we would *still* need a mutex to read the
// totals out.
// TODO: limit maximum size
type PacketCountSummary struct {
	total       PacketCounters
	byPort      map[int]*PacketCounters
	byInterface map[string]*PacketCounters
	mutex       sync.RWMutex
}

func NewPacketCountSummary() *PacketCountSummary {
	return &PacketCountSummary{
		byPort:      make(map[int]*PacketCounters),
		byInterface: make(map[string]*PacketCounters),
	}
}

func (s *PacketCountSummary) Update(c PacketCounters) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if prev, ok := s.byPort[c.SrcPort]; ok {
		prev.Add(c)
	} else {
		new := &PacketCounters{
			Interface: "*",
			SrcPort:   c.SrcPort,
			DstPort:   0,
		}
		new.Add(c)
		s.byPort[new.SrcPort] = new
	}

	if prev, ok := s.byPort[c.DstPort]; ok {
		prev.Add(c)
	} else {
		// Use SrcPort as the identifier in the
		// accumulated counter
		new := &PacketCounters{
			Interface: "*",
			SrcPort:   c.DstPort,
			DstPort:   0,
		}
		new.Add(c)
		s.byPort[new.SrcPort] = new
	}

	if prev, ok := s.byInterface[c.Interface]; ok {
		prev.Add(c)
	} else {
		new := &PacketCounters{
			Interface: c.Interface,
			SrcPort:   0,
			DstPort:   0,
		}
		new.Add(c)
		s.byInterface[new.Interface] = new
	}

	s.total.Add(c)
}

func (s *PacketCountSummary) Total() PacketCounters {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.total
}

// Packet counters summed over interface
func (s *PacketCountSummary) TotalOnInterface(name string) PacketCounters {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if count, ok := s.byInterface[name]; ok {
		return *count
	}

	return PacketCounters{Interface: name}
}

// Packet counters summed over port
func (s *PacketCountSummary) TotalOnPort(port int) PacketCounters {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if count, ok := s.byPort[port]; ok {
		return *count
	}
	return PacketCounters{Interface: "*", SrcPort: port}
}

// All available port numbers
func (s *PacketCountSummary) AllPorts() []PacketCounters {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	ret := make([]PacketCounters, 0, len(s.byPort))
	for _, v := range s.byPort {
		ret = append(ret, *v)
	}
	return ret
}

// Observe every captured TCP segment here
func CountTcpPackets(ifc string, packetCount PacketCountConsumer) pcap.NetworkTrafficObserver {
	observer := func(p gopacket.Packet) {
		if tcpLayer := p.Layer(layers.LayerTypeTCP); tcpLayer != nil {
			tcp, _ := tcpLayer.(*layers.TCP)
			packetCount.Update(PacketCounters{
				Interface:  ifc,
				SrcPort:    int(tcp.SrcPort),
				DstPort:    int(tcp.DstPort),
				TCPPackets: 1,
			})
		}
	}
	return pcap.NetworkTrafficObserver(observer)
}
