package pcap

import (
	"bytes"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/reassembly"

	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/memview"
)

var (
	ip1        = net.IP{1, 2, 3, 4}
	port1      = 1234
	ip2        = net.IP{8, 8, 8, 8}
	port2      = 53
	ip3        = net.IP{127, 0, 0, 1}
	port3      = 8080
	brokerIP   = net.IP{172, 16, 12, 3}
	brokerPort = 55855
)

type fakePcap []gopacket.Packet

func (f fakePcap) capturePackets(done <-chan struct{}, interfaceName, bpfFilter string) (<-chan gopacket.Packet, error) {
	outChan := make(chan gopacket.Packet)
	go func() {
		defer close(outChan)
		for _, p := range f {
			select {
			case <-done:
				return
			case outChan <- p:
			}
		}
	}()
	return outChan, nil
}

func (f fakePcap) getInterfaceAddrs(interfaceName string) ([]net.IP, error) {
	return []net.IP{brokerIP}, nil
}

// Fake pcap implementation that only closes the output channel when explicitly
// cancelled.
type forceCancelPcap []gopacket.Packet

func (f forceCancelPcap) capturePackets(done <-chan struct{}, interfaceName, bpfFilter string) (<-chan gopacket.Packet, error) {
	outChan := make(chan gopacket.Packet)
	go func() {
		defer close(outChan)
		for _, p := range f {
			select {
			case <-done:
				return
			case outChan <- p:
			}
		}
		// Wait for explicit cancellation
		<-done
	}()
	return outChan, nil
}

func (f forceCancelPcap) getInterfaceAddrs(interfaceName string) ([]net.IP, error) {
	return []net.IP{brokerIP}, nil
}

const (
	princeProtoHeader    = "prince|"
	pineappleProtoHeader = "pineapple^"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// Quick and dirty way to see if t1 < t2. For test only.
func netTrafficLess(t1, t2 akinet.ParsedNetworkTraffic) bool {
	return strings.Compare(fmt.Sprintf("%v", t1), fmt.Sprintf("%v", t2)) < 0
}

func princeLess(p1, p2 akinet.AkitaPrince) bool {
	return strings.Compare(string(p1), string(p2)) < 0
}

func byteSliceLess(b1, b2 []byte) bool {
	return bytes.Compare(b1, b2) < 0
}

func stringLess(s1, s2 string) bool {
	return strings.Compare(s1, s2) < 0
}

func compareRawBytes(rb1, rb2 akinet.RawBytes) bool {
	return strings.Compare(rb1.String(), rb2.String()) == 0
}

// Custom cmp.Diff function with common options for use in net_parse related
// tests.
func netParseCmp(e1, e2 interface{}) string {
	return cmp.Diff(e1, e2,
		cmpopts.SortSlices(netTrafficLess),
		cmpopts.SortSlices(princeLess),
		cmpopts.SortSlices(byteSliceLess),
		cmpopts.SortSlices(stringLess),
		cmp.Comparer(compareRawBytes),
	)
}

// Create packet with TCP SYN. This allows the TCP reassembler to create a new
// stream.
func makeTCPSYNPacket(srcIP, dstIP net.IP, srcPort, dstPort int, data []byte) gopacket.Packet {
	synPkt := CreatePacketWithSeq(srcIP, dstIP, srcPort, dstPort, data, uint32(0))
	synPktTcp := synPkt.TransportLayer().(*layers.TCP)
	synPktTcp.SYN = true
	return synPkt
}

// Prince protocol: prince|<payload>|
type princeParserFactory struct{}

func (princeParserFactory) Name() string {
	return "PrinceParserFactory"
}

func (princeParserFactory) Accepts(input memview.MemView, isEnd bool) (decision akinet.AcceptDecision, df int64) {
	defer func() {
		if decision == akinet.NeedMoreData && isEnd {
			decision = akinet.Reject
			df = input.Len()
		}
	}()

	if input.Len() < int64(len(princeProtoHeader)) {
		return akinet.NeedMoreData, 0
	}

	i := input.Index(0, []byte(princeProtoHeader))
	if i < 0 {
		// The proto header could have leading bytes in front of it.
		p := input.Index(0, []byte(princeProtoHeader[0:1]))
		if p >= 0 {
			return akinet.NeedMoreData, input.Len() - p
		}
		return akinet.NeedMoreData, input.Len()
	}
	return akinet.Accept, i
}

func (princeParserFactory) CreateParser(id akinet.TCPBidiID, seq, ack reassembly.Sequence) akinet.TCPParser {
	return &princeParser{}
}

type princeParser struct {
	all memview.MemView
}

func (*princeParser) Name() string {
	return "prince!"
}

// Assumes input starts with the right header.
func (p *princeParser) Parse(input memview.MemView, isEnd bool) (akinet.ParsedNetworkContent, memview.MemView, error) {
	p.all.Append(input)

	barOne := int64(len(princeProtoHeader) - 1)
	if p.all.GetByte(barOne) != '|' {
		return nil, p.all, fmt.Errorf("prince parser got content that does start with prince proto header %s", strconv.Quote(p.all.String()))
	}

	barTwo := p.all.Index(barOne+1, []byte("|"))
	if barTwo < 0 {
		if isEnd {
			return nil, p.all, fmt.Errorf("EOF before parse done")
		}
		// Not done yet.
		return nil, memview.MemView{}, nil
	}

	c := akinet.AkitaPrince(p.all.SubView(barOne+1, barTwo).String())
	unused := p.all.SubView(barTwo+1, p.all.Len())
	return c, unused, nil
}

// Prince protocol: pineapple^<payload>^
type pineappleParserFactory struct{}

func (pineappleParserFactory) Name() string {
	return "PineappleParserFactory"
}

func (pineappleParserFactory) Accepts(input memview.MemView, isEnd bool) (decision akinet.AcceptDecision, df int64) {
	defer func() {
		if decision == akinet.NeedMoreData && isEnd {
			decision = akinet.Reject
			df = input.Len()
		}
	}()

	if input.Len() < int64(len(pineappleProtoHeader)) {
		return akinet.NeedMoreData, 0
	}

	i := input.Index(0, []byte(pineappleProtoHeader))
	if i < 0 {
		// The proto header could have leading bytes in front of it.
		p := input.Index(0, []byte(pineappleProtoHeader[0:1]))
		if p >= 0 {
			return akinet.NeedMoreData, input.Len() - p
		}
		return akinet.NeedMoreData, input.Len()
	}
	return akinet.Accept, i
}

func (pineappleParserFactory) CreateParser(id akinet.TCPBidiID, seq, ack reassembly.Sequence) akinet.TCPParser {
	return pineappleParser{}
}

type pineappleParser struct{}

func (pineappleParser) Name() string {
	return "pineapple!"
}

// Assumes input starts with the right header.
func (pineappleParser) Parse(input memview.MemView, isEnd bool) (akinet.ParsedNetworkContent, memview.MemView, error) {
	return nil, input, fmt.Errorf("should not get invoked")
}
