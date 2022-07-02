package trace

import (
	"sync"

	"github.com/akitasoftware/akita-cli/pcap"
	. "github.com/akitasoftware/akita-libs/client_telemetry"
	"github.com/akitasoftware/go-utils/math"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"
)

// A consumer accepts incremental updates in the form
// of PacketCounters.
type PacketCountConsumer interface {
	// Add an additional measurement to the current count
	Update(delta PacketCounts)
}

// Discard the count
type PacketCountDiscard struct {
}

func (d *PacketCountDiscard) Update(_ PacketCounts) {
}

// A consumer that sums the count by (interface, port) pairs.
// In the future, this could put counters on a pipe and do the increments
// in a separate goroutine, but we would *still* need a mutex to read the
// totals out.
// TODO: limit maximum size
type PacketCounter struct {
	total       PacketCounts
	byPort      map[int]*PacketCounts
	byInterface map[string]*PacketCounts
	mutex       sync.RWMutex
}

func NewPacketCounter() *PacketCounter {
	return &PacketCounter{
		byPort:      make(map[int]*PacketCounts),
		byInterface: make(map[string]*PacketCounts),
	}
}

func (s *PacketCounter) Update(c PacketCounts) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if prev, ok := s.byPort[c.SrcPort]; ok {
		prev.Add(c)
	} else {
		new := &PacketCounts{
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
		new := &PacketCounts{
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
		new := &PacketCounts{
			Interface: c.Interface,
			SrcPort:   0,
			DstPort:   0,
		}
		new.Add(c)
		s.byInterface[new.Interface] = new
	}

	s.total.Add(c)
}

func (s *PacketCounter) Total() PacketCounts {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.total
}

// Packet counters summed over interface
func (s *PacketCounter) TotalOnInterface(name string) PacketCounts {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if count, ok := s.byInterface[name]; ok {
		return *count
	}

	return PacketCounts{Interface: name}
}

// Packet counters summed over port
func (s *PacketCounter) TotalOnPort(port int) PacketCounts {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if count, ok := s.byPort[port]; ok {
		return *count
	}
	return PacketCounts{Interface: "*", SrcPort: port}
}

// All available port numbers
func (s *PacketCounter) AllPorts() []PacketCounts {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	ret := make([]PacketCounts, 0, len(s.byPort))
	for _, v := range s.byPort {
		ret = append(ret, *v)
	}
	return ret
}

// Return a summary of the total, as well as the top N ports and
// interfaces by TCP traffic.
func (s *PacketCounter) Summary(n int) *PacketCountSummary {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return &PacketCountSummary{
		Version:        Version,
		Total:          s.total,
		TopByPort:      topNByTcpPacketCount(s.byPort, n),
		TopByInterface: topNByTcpPacketCount(s.byInterface, n),
	}
}

type pair[T constraints.Ordered] struct {
	k T
	v *PacketCounts
}

// Return a new map with the N entries in counts with the highest TCP packet
// counts.  In the case of a tie for the Nth position, the entry with the
// smallest key is selected.
func topNByTcpPacketCount[T constraints.Ordered](counts map[T]*PacketCounts, n int) map[T]*PacketCounts {
	rv := make(map[T]*PacketCounts, math.Min(len(counts), n))

	pairs := make([]pair[T], 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, pair[T]{k: k, v: v})
	}

	// Sort descending by TCPPackets.
	slices.SortFunc(pairs, func(a, b pair[T]) bool {
		if a.v.TCPPackets != b.v.TCPPackets {
			return b.v.TCPPackets < a.v.TCPPackets
		}
		return a.k < b.k
	})

	// Take the first N pairs and construct the result.
	for i := 0; i < math.Min(len(pairs), n); i++ {
		rv[pairs[i].k] = pairs[i].v
	}

	return rv
}

// Observe every captured TCP segment here
func CountTcpPackets(ifc string, packetCount PacketCountConsumer) pcap.NetworkTrafficObserver {
	observer := func(p gopacket.Packet) {
		if tcpLayer := p.Layer(layers.LayerTypeTCP); tcpLayer != nil {
			tcp, _ := tcpLayer.(*layers.TCP)
			packetCount.Update(PacketCounts{
				Interface:  ifc,
				SrcPort:    int(tcp.SrcPort),
				DstPort:    int(tcp.DstPort),
				TCPPackets: 1,
			})
		}
	}
	return pcap.NetworkTrafficObserver(observer)
}
