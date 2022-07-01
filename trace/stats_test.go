package trace

import (
	"testing"

	. "github.com/akitasoftware/akita-libs/client_telemetry"
	"github.com/stretchr/testify/assert"
)

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
	}

	for _, tc := range tests {
		actual := topNByTcpPacketCount(tc.from, tc.take)
		assert.Equal(t, tc.expected, actual, tc.name)
	}
}
