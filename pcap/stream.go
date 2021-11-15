package pcap

import (
	"encoding/binary"
	"net"
	"sync/atomic"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/reassembly"
	"github.com/google/uuid"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/memview"
)

// These error counters don't seem to have a comfortable home, can we somehow get them back up to the
// normal packet counter?  They can't go in tcpFlow because that's ephemeral.

// Nunmber of times we got a nil assembler context; this can happen when the payload
// resides in a page other than the first in the reassembly buffer.
var CountNilAssemblerContext uint64

// or when we flush old data?
var CountNilAssemblerContextAfterParse uint64

// Number of times we got an assembler context of the wrong type; this probably shouldn't
// happen at all.
var CountBadAssemblerContextType uint64

// tcpFlow represents a uni-directional flow of TCP segments along with a
// bidirectional ID that identifies the tcpFlow in the opposite direction.
// Writes come from TCP assembler via tcpStream, while reads come from users
// of this struct.
type tcpFlow struct {
	clock clockWrapper // constant

	netFlow gopacket.Flow // constant
	tcpFlow gopacket.Flow // constant

	// Shared with tcpFlow in the opposite direction of this flow.
	bidiID akinet.TCPBidiID // constant

	outChan chan<- akinet.ParsedNetworkTraffic

	factorySelector akinet.TCPParserFactorySelector

	// Non-nil if there is an active parser for this flow.
	currentParser akinet.TCPParser

	// Context for the FIRST packet that currentParser is processing.
	currentParserCtx *assemblerCtxWithSeq

	// Data that was left unused when determining parser, awaiting for more data.
	// This is a hack to flush data when the flow terminates before a parser has
	// been selected since reassembled does not get invoked on stream end even if
	// we use KeepFrom to keep data inside ScatterGather in a previous call to
	// reassembled.
	unusedAcceptBuf memview.MemView
}

func newTCPFlow(clock clockWrapper, bidiID akinet.TCPBidiID, nf, tf gopacket.Flow, outChan chan<- akinet.ParsedNetworkTraffic, fs akinet.TCPParserFactorySelector) *tcpFlow {
	return &tcpFlow{
		clock:           clock,
		netFlow:         nf,
		tcpFlow:         tf,
		bidiID:          bidiID,
		outChan:         outChan,
		factorySelector: fs,
	}
}

func (f *tcpFlow) handleUnparseable(t time.Time, d memview.MemView) {
	if d.Len() > 0 {
		f.outChan <- f.toPNT(t, t, akinet.RawBytes(d))
	}
}

// Handles reassmbled TCP flow data.
func (f *tcpFlow) reassembled(sg reassembly.ScatterGather, ac reassembly.AssemblerContext) {
	f.reassembledWithIgnore(0, sg, ac)
}

// Ignore leading bytes from sg.
func (f *tcpFlow) reassembledWithIgnore(ignoreCount int, sg reassembly.ScatterGather, ac reassembly.AssemblerContext) {
	_, _, isEnd, _ := sg.Info()
	bytesAvailable, _ := sg.Lengths()
	// Fetch returns a copy of the packet data.
	pktData := memview.New(sg.Fetch(bytesAvailable)[ignoreCount:])

	printer.V(6).Infof("reassembled with %d bytes, isEnd=%v\n", bytesAvailable-ignoreCount, isEnd)

	if f.currentParser == nil {
		// Try to create a new parser.
		fact, decision, discardFront := f.factorySelector.Select(pktData, isEnd)
		if discardFront > 0 {
			printer.V(6).Infof("discarding %d bytes discarded by all parsers\n", discardFront)
			f.handleUnparseable(sg.CaptureInfo(ignoreCount).Timestamp, pktData.SubView(0, discardFront))
			pktData = pktData.SubView(discardFront, pktData.Len())
		}

		switch decision {
		case akinet.NeedMoreData:
			// Keep data for next reassembled call.
			printer.V(6).Infof("NeedMoreData to determine parser\n")
			sg.KeepFrom(ignoreCount + int(discardFront))
			f.unusedAcceptBuf = pktData
			return
		case akinet.Reject:
			printer.V(6).Infof("Reject by all parsers\n")
			f.unusedAcceptBuf.Clear()
			return
		case akinet.Accept:
			printer.V(6).Infof("Accept by %s\n", fact.Name())
			f.unusedAcceptBuf.Clear()

			acForFirstByte := sg.AssemblerContext(ignoreCount + int(discardFront))
			ctx, ok := acForFirstByte.(*assemblerCtxWithSeq)
			if !ok {
				// Previously we errored in this case:
				printer.V(6).Infof("received AssemblerContext %v without TCP seq info, treating %s data as raw bytes\n", acForFirstByte, fact.Name())
				// but a user ran into quite a lot of them.  One theory is that this occurs when the HTTP response is in the
				// second (or later) page of a reassembly buffer.  A test validates that, but there might be other causes
				// that we don't yet understand.
				// So, track the error count but don't spam the log.
				if acForFirstByte == nil {
					atomic.AddUint64(&CountNilAssemblerContext, 1)
				} else {
					atomic.AddUint64(&CountBadAssemblerContextType, 1)
				}
				f.handleUnparseable(sg.CaptureInfo(ignoreCount).Timestamp, pktData)
				return
			}
			f.currentParser = fact.CreateParser(f.bidiID, ctx.seq, ctx.ack)
			f.currentParserCtx = ctx
		default:
			printer.Errorf("unsupported decision type %s, treating data as raw bytes\n", decision)
			f.handleUnparseable(sg.CaptureInfo(ignoreCount).Timestamp, pktData)
			return
		}
	}

	pnc, unused, err := f.currentParser.Parse(pktData, isEnd)
	if err != nil {
		// Parser failed, return all the bytes passed to the parser so at least we
		// can still perform leak detection on the raw bytes.
		t := f.currentParserCtx.GetCaptureInfo().Timestamp
		f.handleUnparseable(t, unused)

		f.currentParser = nil
		f.currentParserCtx = nil
	} else if pnc != nil {
		// Parsing complete.
		parseStart := f.currentParserCtx.GetCaptureInfo().Timestamp
		var parseEnd time.Time
		if ac != nil {
			parseEnd = ac.GetCaptureInfo().Timestamp
		} else {
			// We could use time.Now() but because this case seems to
			// appear when we have called FlushCloseOlderThan, it would
			// probably be misleading.
			// TODO: what else can we log here to help identify what's going on?
			printer.V(6).Infof("AssemblerContext is nil for packet started at %v\n", parseStart)
			atomic.AddUint64(&CountNilAssemblerContextAfterParse, 1)
			parseEnd = parseStart
		}
		f.outChan <- f.toPNT(parseStart, parseEnd, pnc)

		f.currentParser = nil
		f.currentParserCtx = nil

		if unused.Len() > 0 {
			// Any unused bytes must be from the latest call to Parse, or else Parse
			// would've returned done in the previous call.
			if isEnd {
				// This is the last chance we can parse the unused portion of data.
				// Don't just treat as RawBytes in case 2 pieces of parsable content
				// arrived on the same packet.
				f.reassembledWithIgnore(bytesAvailable-int(unused.Len()), sg, ac)
				return
			} else {
				sg.KeepFrom(bytesAvailable - int(unused.Len()))
			}
		}
	} else {
		// Parsing not done, resume after new reassembled data becomes available.
		// No need to call sg.KeepFrom because all the bytes are held by the parser
		// and returned to us later if the parser runs into an error.
	}
}

// Marks this flow as finished.
func (f *tcpFlow) reassemblyComplete() {
	if f.currentParser != nil {
		// We were in the middle of parsing something, give up.
		pnc, unused, _ := f.currentParser.Parse(memview.New(nil), true)
		t := f.currentParserCtx.GetCaptureInfo().Timestamp
		f.handleUnparseable(t, unused)
		if pnc != nil {
			f.outChan <- f.toPNT(t, t, pnc)
		}
		f.currentParser = nil
		f.currentParserCtx = nil
	} else if f.unusedAcceptBuf.Len() > 0 {
		// The flow terminated before a parser has been selected, flush any bytes
		// that were buffered waiting for more data to determine parse.
		// We estimate the time with current time instead of tracking a separate
		// context since unusedAcceptBuf is unlikely to be used and is almost
		// certainly very small in size.
		f.outChan <- f.toPNT(f.clock.Now(), f.clock.Now(), akinet.RawBytes(f.unusedAcceptBuf))
	}
}

func (f *tcpFlow) toPNT(firstPacketTime time.Time, lastPacketTime time.Time,
	c akinet.ParsedNetworkContent) akinet.ParsedNetworkTraffic {
	if firstPacketTime.IsZero() {
		firstPacketTime = f.clock.Now()
	}
	if lastPacketTime.IsZero() {
		lastPacketTime = firstPacketTime
	}

	// Endpoint interpretation logic from
	// https://github.com/google/gopacket/blob/0ad7f2610e344e58c1c95e2adda5c3258da8e97b/layers/endpoints.go#L30
	srcE, dstE := f.netFlow.Endpoints()
	srcP, dstP := f.tcpFlow.Endpoints()
	return akinet.ParsedNetworkTraffic{
		SrcIP:           net.IP(srcE.Raw()),
		SrcPort:         int(binary.BigEndian.Uint16(srcP.Raw())),
		DstIP:           net.IP(dstE.Raw()),
		DstPort:         int(binary.BigEndian.Uint16(dstP.Raw())),
		Content:         c,
		ObservationTime: firstPacketTime,
		FinalPacketTime: lastPacketTime,
	}
}

// tcpStream represents a pair of uni-directional tcpFlows. It implements
// reassembly.Stream interface to receive reassembled packets for BOTH flows,
// which it then directs to the correct tcpFlow.
type tcpStream struct {
	clock  clockWrapper     // constant
	bidiID akinet.TCPBidiID // constant

	// Network layer flow.
	netFlow gopacket.Flow

	// flows is populated upon seeing the first packet.
	flows map[reassembly.TCPFlowDirection]*tcpFlow

	factorySelector akinet.TCPParserFactorySelector
	outChan         chan<- akinet.ParsedNetworkTraffic
}

func newTCPStream(clock clockWrapper, netFlow gopacket.Flow, outChan chan<- akinet.ParsedNetworkTraffic, fs akinet.TCPParserFactorySelector) *tcpStream {
	return &tcpStream{
		clock:           clock,
		bidiID:          akinet.TCPBidiID(uuid.New()),
		netFlow:         netFlow,
		factorySelector: fs,
		outChan:         outChan,
	}
}

func (c *tcpStream) Accept(tcp *layers.TCP, _ gopacket.CaptureInfo, dir reassembly.TCPFlowDirection, _ reassembly.Sequence, start *bool, ac reassembly.AssemblerContext) bool {
	// We always force the TCP stream to start because we cannot guarantee that we
	// will ever observe the SYN packet. For example, we could be looking at an
	// existing connection that is actively reused by HTTP traffic. Without the
	// forced start, the stream will be held up by the assembler forever and we'll
	// never get a change to analyze its data.
	*start = true

	if c.flows == nil {
		// We are accepting the first packet for this connection.
		// Create the 2 flows now that we know the directionality.
		// We speculatively create a tcpFlow for the opposite direction. Reads
		// from from the speculative flow will block until it receives reassembled
		// data from this tcpStream or it is garbage collected by the assembler
		// after streamTimeout.
		tf, _ := gopacket.FlowFromEndpoints(layers.NewTCPPortEndpoint(tcp.SrcPort), layers.NewTCPPortEndpoint(tcp.DstPort))
		s1 := newTCPFlow(c.clock, c.bidiID, c.netFlow, tf, c.outChan, c.factorySelector)
		s2 := newTCPFlow(c.clock, c.bidiID, c.netFlow.Reverse(), tf.Reverse(), c.outChan, c.factorySelector)
		c.flows = map[reassembly.TCPFlowDirection]*tcpFlow{
			dir:           s1,
			dir.Reverse(): s2,
		}
	}

	// Output some metadata for the current packet.
	{
		srcE, dstE := c.netFlow.Endpoints()
		c.outChan <- akinet.ParsedNetworkTraffic{
			SrcIP:   net.IP(srcE.Raw()),
			SrcPort: int(tcp.SrcPort),
			DstIP:   net.IP(dstE.Raw()),
			DstPort: int(tcp.DstPort),
			Content: akinet.TCPPacketMetadata{
				ConnectionID:        akid.NewConnectionID(uuid.UUID(c.bidiID)),
				SYN:                 tcp.SYN,
				ACK:                 tcp.ACK,
				FIN:                 tcp.FIN,
				RST:                 tcp.RST,
				PayloadLength_bytes: len(tcp.LayerPayload()),
			},
			ObservationTime: ac.GetCaptureInfo().Timestamp,
		}
	}

	// Accept everything, even if the packet might violate the TCP state machine
	// and get rejected by the client or server's TCP stack. We do this because we
	// are interested in detecting all dataflows, not just ones from valid TCP
	// connections.
	// We reassembly library does guarantee to deliver data in stream order, so we
	// don't need to worry about getting out-of-order or duplicate data.
	return true
}

// Handles reassmbled TCP stream data.
func (c *tcpStream) ReassembledSG(sg reassembly.ScatterGather, ac reassembly.AssemblerContext) {
	if c.flows == nil {
		printer.Errorf("received reassembled TCP stream data before accept, dropping packets\n")
		return
	}
	dir, _, _, _ := sg.Info()
	c.flows[dir].reassembled(sg, ac)
}

func (c *tcpStream) ReassemblyComplete(_ reassembly.AssemblerContext) bool {
	for _, s := range c.flows {
		s.reassemblyComplete()
	}

	// Remove connection from the pool
	return true
}
