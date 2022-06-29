package trace

import (
	"github.com/akitasoftware/akita-cli/pcap"
	"github.com/akitasoftware/akita-libs/client_telemetry"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// Observe every captured TCP segment here
func CountTcpPackets(ifc string, packetCount client_telemetry.PacketCountConsumer) pcap.NetworkTrafficObserver {
	observer := func(p gopacket.Packet) {
		if tcpLayer := p.Layer(layers.LayerTypeTCP); tcpLayer != nil {
			tcp, _ := tcpLayer.(*layers.TCP)
			packetCount.Update(client_telemetry.PacketCounters{
				Interface:  ifc,
				SrcPort:    int(tcp.SrcPort),
				DstPort:    int(tcp.DstPort),
				TCPPackets: 1,
			})
		}
	}
	return pcap.NetworkTrafficObserver(observer)
}
