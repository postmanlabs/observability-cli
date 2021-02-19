package apidump

import (
	"net"
	"testing"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/stretchr/testify/assert"
)

type fakeInterface []net.Addr

func (fi fakeInterface) Addrs() ([]net.Addr, error) {
	return []net.Addr(fi), nil
}

func TestGetInboundBPFFilter(t *testing.T) {
	fakeInterfaces := map[string]interfaceInfo{
		"eth0": fakeInterface([]net.Addr{
			&net.IPAddr{IP: net.ParseIP("1.2.3.4")},
		}),
		"lo": fakeInterface([]net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.IPv4Mask(255, 0, 0, 0)},
		}),
	}

	testCases := []struct {
		name      string
		bpfFilter string
		port      uint16
		expected  map[string]string
		expectErr bool
	}{
		{
			name: "no flags, no filter",
			expected: map[string]string{
				"eth0": "",
				"lo":   "",
			},
		},
		{
			name: "with --port",
			port: 25482,
			expected: map[string]string{
				"eth0": "(src host 1.2.3.4 and src port 25482) or (dst host 1.2.3.4 and dst port 25482)",
				"lo":   "(src host 127.0.0.1 and src port 25482) or (dst host 127.0.0.1 and dst port 25482)",
			},
		},
		{
			name:      "with --bpf-filter",
			bpfFilter: "(ether host aa:bb:cc:dd:ee:ff and port 25482) or (ether host 11:22:33:44:55:66 and port 8080)",
			expected: map[string]string{
				"eth0": "(ether host aa:bb:cc:dd:ee:ff and port 25482) or (ether host 11:22:33:44:55:66 and port 8080)",
				"lo":   "(ether host aa:bb:cc:dd:ee:ff and port 25482) or (ether host 11:22:33:44:55:66 and port 8080)",
			},
		},
		{
			name:      "with both --port and --bpf-filter",
			port:      25482,
			bpfFilter: "(ether host aa:bb:cc:dd:ee:ff and port 25482) or (ether host 11:22:33:44:55:66 and port 8080)",
			expectErr: true,
		},
	}
	for _, c := range testCases {
		filters, err := getInboundBPFFilter(fakeInterfaces, c.bpfFilter, c.port)
		if c.expectErr {
			assert.Error(t, err, c.name)
			continue
		}
		assert.NoError(t, err, c.name)

		// Make sure all the BPF filters are well-formed.
		for _, f := range filters {
			_, err := pcap.CompileBPFFilter(layers.LinkTypeEthernet, 100, f)
			assert.NoError(t, err, c.name)
		}

		assert.Equal(t, c.expected, filters, c.name)
	}
}
