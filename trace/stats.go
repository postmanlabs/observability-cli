package trace

import (
	"sync"

	. "github.com/akitasoftware/akita-libs/client_telemetry"
	"github.com/akitasoftware/go-utils/math"
	"github.com/akitasoftware/go-utils/optionals"
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

// A consumer that sums the count by (Interface, SrcPort, SrcHost) tuples.
// (DstPort, DstHost) are unused.
//
// In the future, this could put counters on a pipe and do the increments
// in a separate goroutine, but we would *still* need a mutex to read the
// totals out.
//
// Imposes a hard limit on the number of ports, interfaces, and hosts that
// are individually tracked.
type PacketCounter struct {
	total       PacketCounts
	byPort      *BoundedPacketCounter[int]
	byInterface *BoundedPacketCounter[string]
	mutex       sync.RWMutex

	// XXX(cns): Only counts HTTPRequest and TLSHello.  Other metrics are not
	//   easily tracked per-host.
	byHost *BoundedPacketCounter[string]
}

// The maximum number (each) of ports, interfaces, or hosts that we track.
const maxKeys = 10_000

// Special host name indicating that no host information was available, e.g.
// because TLS 1.3 encrypts SNI data.
const HostnameUnavailable = "(hosts without available names)"

func NewPacketCounter() *PacketCounter {
	return &PacketCounter{
		byPort:      NewBoundedPacketCounter[int](maxKeys),
		byInterface: NewBoundedPacketCounter[string](maxKeys),
		byHost:      NewBoundedPacketCounter[string](maxKeys),
	}
}

func (s *PacketCounter) Update(c PacketCounts) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Add source host if we have host data.
	if c.SrcHost != "" {
		s.byHost.AddOrInsert(c.SrcHost, c, func(c PacketCounts) *PacketCounts {
			new := &PacketCounts{
				Interface: "*",
				SrcHost:   c.SrcHost,
			}
			new.Add(c)
			return new
		})
	}

	// Add dest host if we have host data.
	if c.DstHost != "" {
		s.byHost.AddOrInsert(c.DstHost, c, func(c PacketCounts) *PacketCounts {
			// Use SrcHost as the identifier in the
			// accumulated counter
			new := &PacketCounts{
				Interface: "*",
				SrcHost:   c.DstHost,
			}
			new.Add(c)
			return new
		})
	}

	// Add source port.
	s.byPort.AddOrInsert(c.SrcPort, c, func(c PacketCounts) *PacketCounts {
		new := &PacketCounts{
			Interface: "*",
			SrcPort:   c.SrcPort,
			DstPort:   0,
		}
		new.Add(c)
		return new
	})

	// Add dest port.
	s.byPort.AddOrInsert(c.DstPort, c, func(c PacketCounts) *PacketCounts {
		// Use SrcPort as the identifier in the
		// accumulated counter
		new := &PacketCounts{
			Interface: "*",
			SrcPort:   c.DstPort,
			DstPort:   0,
		}
		new.Add(c)
		return new
	})

	// Add interface.
	s.byInterface.AddOrInsert(c.Interface, c, func(c PacketCounts) *PacketCounts {
		new := &PacketCounts{
			Interface: c.Interface,
			SrcPort:   0,
			DstPort:   0,
		}
		new.Add(c)
		return new
	})

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
	if count, ok := s.byInterface.Get(name); ok {
		return *count
	}

	return PacketCounts{Interface: name}
}

// Packet counters summed over port
func (s *PacketCounter) TotalOnPort(port int) PacketCounts {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if count, ok := s.byPort.Get(port); ok {
		return *count
	}
	return PacketCounts{Interface: "*", SrcPort: port}
}

// Packet counters summed over host
func (s *PacketCounter) TotalOnHost(host string) PacketCounts {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if count, ok := s.byHost.Get(host); ok {
		return *count
	}
	return PacketCounts{Interface: "*", SrcHost: host}
}

// All available port numbers
func (s *PacketCounter) AllPorts() []PacketCounts {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	ret := make([]PacketCounts, 0, s.byPort.Len())
	for _, v := range s.byPort.RawMap() {
		ret = append(ret, *v)
	}
	return ret
}

// Return a summary of the total, as well as the top N ports and
// interfaces by TCP traffic.
func (s *PacketCounter) Summary(n int) *PacketCountSummary {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	topByPort, byPortOverflow := s.byPort.TopN(n, func(c *PacketCounts) int { return c.TCPPackets })
	topByInterface, byInterfaceOverflow := s.byInterface.TopN(n, func(c *PacketCounts) int { return c.TCPPackets })
	topByHost, byHostOverflow := s.byHost.TopN(n, func(c *PacketCounts) int { return c.HTTPRequests + c.TLSHello })

	var byPortOverflowPtr *PacketCounts
	if overflow, exists := byPortOverflow.Get(); exists {
		byPortOverflowPtr = &overflow
	}

	var byInterfaceOverflowPtr *PacketCounts
	if overflow, exists := byInterfaceOverflow.Get(); exists {
		byInterfaceOverflowPtr = &overflow
	}

	var byHostOverflowPtr *PacketCounts
	if overflow, exists := byHostOverflow.Get(); exists {
		byHostOverflowPtr = &overflow
	}

	return &PacketCountSummary{
		Version:        Version,
		Total:          s.total,
		TopByPort:      topByPort,
		TopByInterface: topByInterface,
		TopByHost:      topByHost,

		ByPortOverflowLimit:      maxKeys,
		ByInterfaceOverflowLimit: maxKeys,
		ByHostOverflowLimit:      maxKeys,

		ByPortOverflow:      byPortOverflowPtr,
		ByInterfaceOverflow: byInterfaceOverflowPtr,
		ByHostOverflow:      byHostOverflowPtr,
	}
}

type pair[T constraints.Ordered] struct {
	k T
	v *PacketCounts
}

type BoundedPacketCounter[T constraints.Ordered] struct {
	// Maximum entries allowed in m.
	limit int

	// Counts extras beyond limit.
	overflow PacketCounts

	m map[T]*PacketCounts
}

// Creates a bounded packet counter limited to `limit` entries.
func NewBoundedPacketCounter[T constraints.Ordered](limit int) *BoundedPacketCounter[T] {
	return &BoundedPacketCounter[T]{
		limit: limit,
		m:     make(map[T]*PacketCounts),

		// Accumulate across all hosts, interfaces and ports after reaching the
		// limit.
		overflow: PacketCounts{
			Interface: "*",
			SrcPort:   0,
			DstPort:   0,
		},
	}
}

// Adds c to m[key]. Inserts makeNew(c) when adding a new key. Adds to
// overflow instead if the limit has been reached.
//
// Use makeNew() to control the contents of the first PacketCount inserted for
// each new key, e.g. to set Interface = "*" when counting by port.
func (bc *BoundedPacketCounter[T]) AddOrInsert(key T, c PacketCounts, makeNew func(PacketCounts) *PacketCounts) {
	if prev, ok := bc.m[key]; ok {
		prev.Add(c)
	} else if bc.HasReachedLimit() {
		bc.overflow.Add(c)
	} else {
		bc.m[key] = makeNew(c)
	}
}

func (bc *BoundedPacketCounter[T]) Get(key T) (*PacketCounts, bool) {
	v, ok := bc.m[key]
	return v, ok
}

// Return the overflow if the size hit the limit or None otherwise.
func (bc *BoundedPacketCounter[T]) GetOverflow() optionals.Optional[PacketCounts] {
	if bc.HasReachedLimit() {
		return optionals.Some(bc.overflow)
	}
	return optionals.None[PacketCounts]()
}

func (bc *BoundedPacketCounter[T]) Len() int {
	return len(bc.m)
}

func (bc *BoundedPacketCounter[T]) RawMap() map[T]*PacketCounts {
	return bc.m
}

// Return a new map with the N entries in counts with the highest TCP packet
// counts.  In the case of a tie for the Nth position, the entry with the
// smallest key is selected.
//
// Returns the overflow count in overflow, or None if there is no overflow.
func (bc *BoundedPacketCounter[T]) TopN(n int, project func(*PacketCounts) int) (rv map[T]*PacketCounts, overflow optionals.Optional[PacketCounts]) {
	rv = make(map[T]*PacketCounts, math.Min(len(bc.m), n))

	pairs := make([]pair[T], 0, len(bc.m))
	for k, v := range bc.m {
		pairs = append(pairs, pair[T]{k: k, v: v})
	}

	// Sort descending.
	slices.SortFunc(pairs, func(a, b pair[T]) bool {
		av := project(a.v)
		bv := project(b.v)
		if av != bv {
			return bv < av
		}
		return a.k < b.k
	})

	// Take the first N pairs and construct the result.
	for i := 0; i < math.Min(len(pairs), n); i++ {
		rv[pairs[i].k] = pairs[i].v
	}

	return rv, bc.GetOverflow()
}

func (bc *BoundedPacketCounter[T]) HasReachedLimit() bool {
	return bc.limit <= len(bc.m)
}
