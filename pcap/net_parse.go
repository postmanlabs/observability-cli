package pcap

import (
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/reassembly"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/memview"
)

const (
	streamTimeout = time.Minute * 5
)

// Internal implementation of reassembly.AssemblerContext that include TCP
// seq and ack numbers.
type assemblerCtxWithSeq struct {
	ci       gopacket.CaptureInfo
	seq, ack reassembly.Sequence
}

func (ctx *assemblerCtxWithSeq) GetCaptureInfo() gopacket.CaptureInfo {
	return ctx.ci
}

// tcpStreamFactory implements reassembly.StreamFactory.
type tcpStreamFactory struct {
	clock   clockWrapper
	fs      akinet.TCPParserFactorySelector
	outChan chan<- akinet.ParsedNetworkTraffic
}

func newTCPStreamFactory(clock clockWrapper, outChan chan<- akinet.ParsedNetworkTraffic, fs akinet.TCPParserFactorySelector) *tcpStreamFactory {
	return &tcpStreamFactory{
		clock:   clock,
		fs:      fs,
		outChan: outChan,
	}
}

func (fact *tcpStreamFactory) New(netFlow, tcpFlow gopacket.Flow, _ *layers.TCP, _ reassembly.AssemblerContext) reassembly.Stream {
	return newTCPStream(fact.clock, netFlow, fact.outChan, fact.fs)
}

type NetworkTrafficObserver func(gopacket.Packet)

type NetworkTrafficParser struct {
	pcap  pcapWrapper
	clock clockWrapper

	observer NetworkTrafficObserver
}

func NewNetworkTrafficParser() *NetworkTrafficParser {
	return &NetworkTrafficParser{
		pcap:     &pcapImpl{},
		clock:    &realClock{},
		observer: func(gopacket.Packet) {},
	}
}

func (p *NetworkTrafficParser) InstallObserver(observer NetworkTrafficObserver) {
	p.observer = observer
}

// Parses network traffic from an interface.
// This function will attempt to parse the traffic with the highest level of
// protocol details as possible. For instance, it will try to piece together
// HTTP request and response pairs.
// The order of parsers matters: earlier parsers will get tried first. Once a
// parser has been accepted, no other parser will be used.
func (p *NetworkTrafficParser) ParseFromInterface(interfaceName, bpfFilter string, signalClose <-chan struct{}, fs ...akinet.TCPParserFactory) (<-chan akinet.ParsedNetworkTraffic, error) {
	// Read in packets, pass to assembler
	packets, err := p.pcap.capturePackets(signalClose, interfaceName, bpfFilter)
	if err != nil {
		return nil, errors.Wrapf(err, "failed begin capturing packets from %s", interfaceName)
	}

	// Set up assembly
	out := make(chan akinet.ParsedNetworkTraffic, 100)
	streamFactory := newTCPStreamFactory(p.clock, out, akinet.TCPParserFactorySelector(fs))
	streamPool := reassembly.NewStreamPool(streamFactory)
	assembler := reassembly.NewAssembler(streamPool)

	go func() {
		ticker := time.NewTicker(streamTimeout / 4)

		defer func() {
			ticker.Stop()

			// Flushes and closes all remaining connections. This should trigger all
			// parsers to hit EOF and return. This call will block until the parsers
			// have returned because tcpStream.ReassemblyComplete waits for
			// parsers.
			assembler.FlushAll()

			// Signal caller that we're done here.
			close(out)
		}()

		for {
			select {
			// packets channel is going to read until EOF or when signalClose is
			// invoked.
			case packet, more := <-packets:
				if !more || packet == nil {
					return
				}
				p.observer(packet)
				p.packetToParsedNetworkTraffic(out, assembler, packet)
			case <-ticker.C:
				// The assembler stops reassembly for streams older than stream timeout.
				// This means the corresponding tcpFlow readers will return EOF.
				assembler.FlushCloseOlderThan(p.clock.Now().Add(-streamTimeout))
			}
		}
	}()

	return out, nil
}

func (p *NetworkTrafficParser) packetToParsedNetworkTraffic(out chan<- akinet.ParsedNetworkTraffic, assembler *reassembly.Assembler, packet gopacket.Packet) {
	if packet.NetworkLayer() == nil {
		printer.V(4).Debugf("unusable packet without network layer\n")
		return
	}

	observationTime := p.clock.Now()
	if packet.Metadata() != nil {
		// Use the more precise timestamp on the packet, if available.
		if t := packet.Metadata().Timestamp; !t.IsZero() {
			observationTime = t
		}
	}

	var srcIP, dstIP net.IP
	switch l := packet.NetworkLayer().(type) {
	case *layers.IPv4:
		srcIP = l.SrcIP
		dstIP = l.DstIP
	case *layers.IPv6:
		srcIP = l.SrcIP
		dstIP = l.DstIP
	}

	if packet.TransportLayer() == nil {
		out <- akinet.ParsedNetworkTraffic{
			SrcIP:           srcIP,
			DstIP:           dstIP,
			Content:         akinet.RawBytes(memview.New(packet.NetworkLayer().LayerPayload())),
			ObservationTime: observationTime,
		}
		return
	}

	switch t := packet.TransportLayer().(type) {
	case *layers.TCP:
		// Let TCP reassembler do extra magic to parse out higher layer protocols.
		assembler.AssembleWithContext(packet.NetworkLayer().NetworkFlow(), t, contextFromTCPPacket(packet, t))
	case *layers.UDP:
		out <- akinet.ParsedNetworkTraffic{
			SrcIP:           srcIP,
			SrcPort:         int(t.SrcPort),
			DstIP:           dstIP,
			DstPort:         int(t.DstPort),
			Content:         akinet.RawBytes(memview.New(t.LayerPayload())),
			ObservationTime: observationTime,
		}
	default:
		out <- akinet.ParsedNetworkTraffic{
			SrcIP:           srcIP,
			DstIP:           dstIP,
			Content:         akinet.RawBytes(memview.New(t.LayerPayload())),
			ObservationTime: observationTime,
		}
	}
}

func contextFromTCPPacket(p gopacket.Packet, t *layers.TCP) *assemblerCtxWithSeq {
	return &assemblerCtxWithSeq{
		ci:  p.Metadata().CaptureInfo,
		seq: reassembly.Sequence(t.Seq),
		ack: reassembly.Sequence(t.Ack),
	}
}
