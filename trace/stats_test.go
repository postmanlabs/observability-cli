package trace

import (
	"testing"

	. "github.com/akitasoftware/akita-libs/client_telemetry"
	"github.com/akitasoftware/go-utils/optionals"
	"github.com/stretchr/testify/assert"
)

func TestCountByPort(t *testing.T) {
	// Make enough ports to exceed the limit.
	limitPlus1Inputs := make([]PacketCounts, maxKeys+1)
	limitPlus1Expected := make(map[int]*PacketCounts, maxKeys)
	for i := range limitPlus1Inputs {
		limitPlus1Inputs[i] = PacketCounts{
			Interface:  "*",
			SrcPort:    i,
			DstPort:    i,
			TCPPackets: 1,
		}
		if i < maxKeys {
			limitPlus1Expected[i] = &PacketCounts{
				Interface:  "*",
				SrcPort:    i,
				TCPPackets: 2,
			}
		}
	}

	tests := []struct {
		name             string
		input            []PacketCounts
		limit            int
		expected         map[int]*PacketCounts
		expectedOverflow optionals.Optional[PacketCounts]
	}{
		{
			name: "init",
			input: []PacketCounts{{
				Interface:  "lo0",
				SrcPort:    1,
				DstPort:    2,
				TCPPackets: 3,
			}},
			limit: 100,
			expected: map[int]*PacketCounts{
				1: {
					Interface:  "*",
					SrcPort:    1,
					TCPPackets: 3,
				},
				2: {
					Interface:  "*",
					SrcPort:    2,
					TCPPackets: 3,
				},
			},
		},
		{
			name:     "limit + 1",
			input:    limitPlus1Inputs,
			expected: limitPlus1Expected,
			expectedOverflow: optionals.Some(PacketCounts{
				Interface:  "*",
				TCPPackets: 2,
			}),
		},
	}

	for _, tc := range tests {
		c := NewPacketCounter()
		for _, counts := range tc.input {
			c.Update(counts)
		}

		// Set a summary above the limit to ensure we get all the ports.
		assert.Equal(t, tc.expected, c.byPort.RawMap(), "["+tc.name+"] raw map")
		assert.Equal(t, tc.expectedOverflow, c.byPort.GetOverflow(), "["+tc.name+"] overflow")
	}
}

func TestTopNTCP(t *testing.T) {
	tests := []struct {
		name     string
		take     int
		from     map[int]*PacketCounts
		expected map[int]*PacketCounts
	}{
		{
			name:     "empty",
			take:     5,
			from:     map[int]*PacketCounts{},
			expected: map[int]*PacketCounts{},
		},
		{
			name:     "take nothing",
			take:     0,
			from:     map[int]*PacketCounts{80: &PacketCounts{}},
			expected: map[int]*PacketCounts{},
		},
		{
			name: "take one from many",
			take: 1,
			from: map[int]*PacketCounts{
				1: &PacketCounts{TCPPackets: 1},
				2: &PacketCounts{TCPPackets: 2},
				3: &PacketCounts{TCPPackets: 3},
			},
			expected: map[int]*PacketCounts{
				3: &PacketCounts{TCPPackets: 3},
			},
		},
		{
			name: "take two from many",
			take: 2,
			from: map[int]*PacketCounts{
				6: &PacketCounts{TCPPackets: 1},
				5: &PacketCounts{TCPPackets: 2},
				4: &PacketCounts{TCPPackets: 3},
			},
			expected: map[int]*PacketCounts{
				5: &PacketCounts{TCPPackets: 2},
				4: &PacketCounts{TCPPackets: 3},
			},
		},
		{
			name:     "take two from one",
			take:     2,
			from:     map[int]*PacketCounts{80: &PacketCounts{}},
			expected: map[int]*PacketCounts{80: &PacketCounts{}},
		},
		{
			name: "take one from many, all equal",
			take: 1,
			from: map[int]*PacketCounts{
				1: &PacketCounts{TCPPackets: 1},
				2: &PacketCounts{TCPPackets: 1},
				3: &PacketCounts{TCPPackets: 1},
			},
			expected: map[int]*PacketCounts{1: &PacketCounts{TCPPackets: 1}},
		},
	}

	for _, tc := range tests {
		bc := &BoundedPacketCounter[int]{
			limit: 100,
			m:     tc.from,
		}
		actual, _ := bc.TopN(tc.take, func(c *PacketCounts) int { return c.TCPPackets })
		assert.Equal(t, tc.expected, actual, tc.name)
	}
}
