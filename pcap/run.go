package pcap

import (
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-libs/akinet"
	akihttp "github.com/akitasoftware/akita-libs/akinet/http"
	"github.com/akitasoftware/akita-libs/akinet/tls"
	"github.com/akitasoftware/akita-libs/buffer_pool"
	. "github.com/akitasoftware/akita-libs/client_telemetry"
)

func Collect(
	stop <-chan struct{},
	intf string,
	bpfFilter string,
	bufferShare float32,
	parseTCPAndTLS bool,
	proc trace.Collector,
	packetCount trace.PacketCountConsumer,
	pool buffer_pool.BufferPool,
) error {
	defer proc.Close()

	facts := []akinet.TCPParserFactory{
		akihttp.NewHTTPRequestParserFactory(pool),
		akihttp.NewHTTPResponseParserFactory(pool),
	}
	if parseTCPAndTLS {
		facts = append(facts,
			tls.NewTLSClientParserFactory(),
			tls.NewTLSServerParserFactory(),
		)
	}

	parser := NewNetworkTrafficParser(bufferShare)

	if packetCount != nil {
		parser.InstallObserver(CountTcpPackets(intf, packetCount))
	}

	parsedChan, err := parser.ParseFromInterface(intf, bpfFilter, stop, facts...)
	if err != nil {
		return errors.Wrap(err, "couldn't start parsing from interface")
	}

	for t := range parsedChan {
		t.Interface = intf
		err := proc.Process(t)
		t.Content.ReleaseBuffers()
		if err != nil {
			return err
		}
	}

	return nil
}

// Observe every captured TCP segment here
func CountTcpPackets(ifc string, packetCount trace.PacketCountConsumer) NetworkTrafficObserver {
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
	return NetworkTrafficObserver(observer)
}
