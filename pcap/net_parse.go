package pcap

import (
	"fmt"
	"net"
	"runtime/debug"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/reassembly"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/akita-libs/akinet"
)

// The maximum time we will wait before flushing a connection and delivering
// the data even if there is a gap in the collected sequence.
var StreamTimeoutSeconds int64 = 10

// The maximum time we will leave a connection open waiting for traffic.
// 90 seconds is the longest possible that the upper layers can wait for
// a response before the request is uploaded before it.  (But this might
// happen as soon as 60 seconds.)
var StreamCloseTimeoutSeconds int64 = 90

// Maximum size of gopacket reassembly buffers, per interface and direction.
//
// A gopacket page is 1900 bytes.
// We want to cap the total memory usage at about 200MB = 105263 pages
var MaxBufferedPagesTotal int = 100_000

// What is a reasonable worst case? We should have enough so that if the
// packet is retransmitted, we will get it before giving up.
// 10Gb/s networking * 1ms RTT = 1.25 MB = 1Gb/s networking * 10ms RTT
// We have observed 3GB growth in RSS over 40 seconds = 75MByte/s
// Assuming a very long 100ms RTT then we'd need 75MB/s * 100ms = 7.5 MB
// 7.5MB / 1900 bytes = 3947 pages
// This would permit only 37 connections to simultaneously stall;
// 1.5MB / 1900 bytes = 657 pages might be better.
// TODO: Would be interesting to know the TCP window sizes we see in practice
// and adjust that way.
var MaxBufferedPagesPerConnection int = 4_000

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

// NetworkTrafficObserver is the callback function type for observing
// packets as they come in to a NetworkTrafficParser.
type NetworkTrafficObserver func(gopacket.Packet)

type NetworkTrafficParser struct {
	pcap        pcapWrapper
	clock       clockWrapper
	observer    NetworkTrafficObserver // This function is called for every packet.
	bufferShare float32
}

func NewNetworkTrafficParser(bufferShare float32) *NetworkTrafficParser {
	return &NetworkTrafficParser{
		pcap:        &pcapImpl{},
		clock:       &realClock{},
		observer:    func(gopacket.Packet) {},
		bufferShare: bufferShare,
	}
}

// Replace the current per-packet callback. Should be called before starting
// ParseFromInterface.
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

	// Override the assembler configuration. (This is the documented way to change them.)
	// Give this particular assembler a fraction of the total pages; there doesn't seem to be a way
	// to set an aggregate limit without major work.
	assembler.AssemblerOptions.MaxBufferedPagesTotal = int(p.bufferShare * float32(MaxBufferedPagesTotal))
	assembler.AssemblerOptions.MaxBufferedPagesPerConnection = MaxBufferedPagesPerConnection

	streamFlushTimeout := time.Duration(StreamTimeoutSeconds) * time.Second
	streamCloseTimeout := time.Duration(StreamCloseTimeoutSeconds) * time.Second

	go func() {
		ticker := time.NewTicker(streamFlushTimeout / 4)
		defer ticker.Stop()

		// Signal caller that we're done on exit
		defer close(out)

		for {
			select {
			// packets channel is going to read until EOF or when signalClose is
			// invoked.
			case packet, more := <-packets:
				if !more || packet == nil {
					// Flushes and closes all remaining connections. This should trigger all
					// parsers to hit EOF and return. This call will block until the parsers
					// have returned because tcpStream.ReassemblyComplete waits for
					// parsers.
					//
					// This is not safe to call in a defer, because it will be called on abnormal
					// exit from FlushCloseOlderThan (like a parser segfault) but assembler might
					// not be in a safe state to call (like holding a mutex.)
					assembler.FlushAll()

					return
				}
				p.observer(packet)
				p.packetToParsedNetworkTraffic(out, assembler, packet)
			case <-ticker.C:
				// The assembler stops reassembly for streams older than streamFlushTimeout.
				// This means the corresponding tcpFlow readers will return EOF.
				//
				// If there is a missing portion of the TCP reassembly (usually due to an
				// uncaptured packet) older then the stream timeout, then this call forces
				// the assembler to skip the missing data and deliver what it has accumulated
				// after that point. The stream will not be closed if it has received
				// packets more recently than that gap.
				//
				// TODO: is this maybe the source of splices, too?  Converting dropped packets
				// into a continous stream?
				//
				// Streams that are idle need to be closed eventually, too.  We use a larger
				// threshold for that because it costs us less memory to keep just a
				// connection record, rather than a backlog of data in the reassembly buffer.
				now := p.clock.Now()
				streamFlushThreshold := now.Add(-streamFlushTimeout)
				streamCloseThreshold := now.Add(-streamCloseTimeout)
				flushed, closed := assembler.FlushWithOptions(
					reassembly.FlushOptions{
						T:  streamFlushThreshold,
						TC: streamCloseThreshold,
					})

				if flushed != 0 || closed != 0 {
					printer.Debugf("%d flushed, %d closed\n", flushed, closed)
				}
			}
		}
	}()

	return out, nil
}

func (p *NetworkTrafficParser) packetToParsedNetworkTraffic(out chan<- akinet.ParsedNetworkTraffic, assembler *reassembly.Assembler, packet gopacket.Packet) {
	defer func() {
		// If we panic during packet handling, do not crash the program. Instead log the error and backtrace.
		// We can perform selective error-handling based on the type of the object passed to panic(),
		// but we can't choose not to recover from certain errors; we would have to re-panic.
		//
		// TODO: detect repeated crashes?
		if err := recover(); err != nil {
			if e, ok := err.(error); ok {
				telemetry.RateLimitError("packet handling", e)
			} else {
				telemetry.RateLimitError("packet handling", fmt.Errorf("%v", err))
			}
			printer.Stderr.Errorf("Panic during packet handling: %v\n%v\n", err, string(debug.Stack()))
		}
	}()

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
			Content:         akinet.DroppedBytes(len(packet.NetworkLayer().LayerPayload())),
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
			Content:         akinet.DroppedBytes(len(t.LayerPayload())),
			ObservationTime: observationTime,
		}
	default:
		out <- akinet.ParsedNetworkTraffic{
			SrcIP:           srcIP,
			DstIP:           dstIP,
			Content:         akinet.DroppedBytes(len(t.LayerPayload())),
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
