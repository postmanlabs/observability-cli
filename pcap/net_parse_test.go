package pcap

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/gopacket"

	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/memview"
)

var (
	testEndpoint1  = &testEndpoint{ip1, port1}
	testEndpoint2  = &testEndpoint{ip2, port2}
	brokerEndpoint = &testEndpoint{brokerIP, brokerPort}
	testTime       = mustParseTime("2020-02-19T15:04:05+08:00")
)

func mustParseTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err != nil {
		panic(err)
	} else {
		return t
	}
}

type testEndpoint struct {
	ip   net.IP
	port int
}

func (te *testEndpoint) String() string {
	return fmt.Sprintf("%v:%v", te.ip, te.port)
}

type testMessage struct {
	from *testEndpoint
	to   *testEndpoint
	data []byte
}

func (tm *testMessage) endpointKey() string {
	return fmt.Sprintf("%s:%s", tm.from.String(), tm.to.String())
}

func (tm *testMessage) reverseEndpointKey() string {
	return fmt.Sprintf("%s:%s", tm.to.String(), tm.from.String())
}

// Helper function to create TCP packets with artificial SYN and SYN-ACK
// injected for connection establishment.
func makeTCPPackets(split int, ms ...*testMessage) []gopacket.Packet {
	pkts := []gopacket.Packet{}
	seqMap := map[string]int{}

	for _, m := range ms {
		endpointKey := m.endpointKey()
		seqN := seqMap[endpointKey]
		if seqN == 0 {
			if _, hasReverse := seqMap[m.reverseEndpointKey()]; hasReverse {
				// We've seen the flow in the opposite direction, so we're the "server".
				pkts = append(pkts, CreateTCPSYNAndACK(m.from.ip, m.to.ip, m.from.port, m.to.port, uint32(0)))
			} else {
				// We're the client initiating the SYN.
				pkts = append(pkts, CreateTCPSYN(m.from.ip, m.to.ip, m.from.port, m.to.port, uint32(0)))
			}
			seqN++
		}
		for _, bs := range bytes.SplitN(m.data, []byte{}, split) {
			aPkt := CreatePacketWithSeq(m.from.ip, m.to.ip, m.from.port, m.to.port, bs, uint32(seqN))
			pkts = append(pkts, aPkt)
			seqN = seqN + len(bs)
		}
		seqMap[endpointKey] = seqN
	}

	return pkts
}

func makeUDPPackets(split int, ms ...*testMessage) []gopacket.Packet {
	pkts := []gopacket.Packet{}
	for _, m := range ms {
		for _, bs := range bytes.SplitN(m.data, []byte{}, split) {
			aPkt := CreateUDPPacket(m.from.ip, m.to.ip, m.from.port, m.to.port, bs)
			pkts = append(pkts, aPkt)
		}
	}
	return pkts
}

func setupParseFromInterface(pcap pcapWrapper, signalClose <-chan struct{}, facts ...akinet.TCPParserFactory) (<-chan akinet.ParsedNetworkTraffic, error) {
	p := NewNetworkTrafficParser(1.0)
	p.pcap = pcap
	p.clock = &fakeClock{testTime}
	return p.ParseFromInterface("dummy0", "", signalClose, facts...)
}

func TestTCPSingleDirection(t *testing.T) {
	// setup the test msg
	msgData1 := []byte("prince|hi how are you this is prince speaking just so you know|")
	msg := &testMessage{testEndpoint1, testEndpoint2, msgData1}

	// setup interface listener
	closeChan := make(chan struct{})
	defer close(closeChan)
	outChan, err := setupParseFromInterface(fakePcap(makeTCPPackets(1, msg)), closeChan, princeParserFactory{}, pineappleParserFactory{})
	if err != nil {
		t.Errorf("unexpected error setting up listener: %v", err)
	}

	nt := <-outChan
	if p, ok := nt.Content.(akinet.AkitaPrince); !ok {
		t.Errorf("expected protocol prince, got %T", nt.Content)
	} else if diff := netParseCmp(p, akinet.AkitaPrince("hi how are you this is prince speaking just so you know")); diff != "" {
		t.Errorf("payload corrupted: %s", diff)
	}
}

func TestTCPBidiStream(t *testing.T) {
	msgData := []byte("prince|hi how are you i am doing very well thank you|")
	msg := &testMessage{testEndpoint1, testEndpoint2, msgData}
	msgRespData := []byte("prince|pineapples talk to princes by eating them and not the other way around|")
	msgResp := &testMessage{testEndpoint2, testEndpoint1, msgRespData}

	closeChan := make(chan struct{})
	defer close(closeChan)
	pkts := makeTCPPackets(3, msg, msgResp)
	out, err := setupParseFromInterface(fakePcap(pkts), closeChan, princeParserFactory{})
	if err != nil {
		t.Fatalf("unexpected error setting up listener: %v", err)
	}

	expectedPrinces := []akinet.AkitaPrince{
		akinet.AkitaPrince("hi how are you i am doing very well thank you"),
		akinet.AkitaPrince("pineapples talk to princes by eating them and not the other way around"),
	}

	princes := make([]akinet.AkitaPrince, 0, 2)
	for nt := range out {
		if p, ok := nt.Content.(akinet.AkitaPrince); !ok {
			t.Fatalf("returned content is not of type 'akinet.AkitaPrince', got %T", nt.Content)
		} else {
			princes = append(princes, p)
		}
	}
	if diff := netParseCmp(expectedPrinces, princes); diff != "" {
		t.Errorf("akinet.AkitaPrince convo mismatch: %s", diff)
	}
}

// If we can't parse any higher level protocol out of a TCP flow, we should
// automatically fallback to output raw bytes.
func TestTCPFallbackToRaw(t *testing.T) {
	msgData := []byte("0ce5908d-f835-42ab-a37c-1152cbc46424")
	msg := &testMessage{testEndpoint1, testEndpoint2, msgData}
	msgRespData := []byte("29ee29a9-4546-4d75-a50a-7bedc79c44ca")
	msgResp := &testMessage{testEndpoint2, testEndpoint1, msgRespData}

	closeChan := make(chan struct{})
	defer close(closeChan)
	pkts := makeTCPPackets(1, msg, msgResp)
	out, err := setupParseFromInterface(fakePcap(pkts), closeChan, princeParserFactory{})
	if err != nil {
		t.Fatalf("unexpected error setting up listener: %v", err)
	}

	expectedRawBytes := []string{
		"0ce5908d-f835-42ab-a37c-1152cbc46424",
		"29ee29a9-4546-4d75-a50a-7bedc79c44ca",
	}

	actual := make([]string, 0, 2)
	for nt := range out {
		if b, ok := nt.Content.(akinet.RawBytes); !ok {
			t.Fatalf("returned content is not of type 'RawBytes', got %T", nt.Content)
		} else {
			actual = append(actual, b.String())
		}
	}
	if diff := netParseCmp(expectedRawBytes, actual); diff != "" {
		t.Errorf("prince convo mismatch: %s", diff)
	}
}

// Test the scenario where the client establishes a TCP session but the server
// speaks first.
func TestTCPServerSpeaksFirst(t *testing.T) {
	// Request 1 is empty - used to initiate the connection. The server will
	// respond with resp1 first before the client speaks req2 back.
	req1 := &testMessage{testEndpoint1, testEndpoint2, []byte{}}
	req2 := &testMessage{testEndpoint1, testEndpoint2, []byte("prince|good boy|")}
	resp1 := &testMessage{testEndpoint2, testEndpoint1, []byte("prince|hello this is prince server|")}
	pkts := makeTCPPackets(1, req1, resp1, req2)

	closeChan := make(chan struct{})
	defer close(closeChan)
	out, err := setupParseFromInterface(fakePcap(pkts), closeChan, princeParserFactory{})
	if err != nil {
		t.Fatalf("unexpected error setting up listener: %v", err)
	}

	expectedPrinces := []akinet.AkitaPrince{
		akinet.AkitaPrince("hello this is prince server"),
		akinet.AkitaPrince("good boy"),
	}

	princes := make([]akinet.AkitaPrince, 0, 2)
	for nt := range out {
		if p, ok := nt.Content.(akinet.AkitaPrince); !ok {
			t.Fatalf("returned content is not of type 'prince', got %T", nt.Content)
		} else {
			princes = append(princes, p)
		}
	}
	if diff := netParseCmp(expectedPrinces, princes); diff != "" {
		t.Errorf("prince convo mismatch: %s", diff)
	}
}

// Test the scenario where we are observing TCP flows in the middle of a stream
// (e.g. packet capture on an existing TCP connection being reused by HTTP). The
// implication is that we will never see the SYN packet, so we need to configure
// the reassembly library to force the stream to start without a SYN.
func TestTCPMidStream(t *testing.T) {
	pkts := []gopacket.Packet{
		// Simulate seeing the trailing bytes of a previous conversation in prince
		// protocol, followed by the response to the new conversation.
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("a|"), 998),            // response 0
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("prince|hello|"), 100), // request 1
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("prince|bye|"), 1000),  // response 1
	}

	closeChan := make(chan struct{})
	defer close(closeChan)
	out, err := setupParseFromInterface(fakePcap(pkts), closeChan, princeParserFactory{})
	if err != nil {
		t.Errorf("unexpected error setting up listener: %v", err)
		return
	}

	// Map src endpoint to bytes received for the flow.
	actual := make(map[string][]akinet.ParsedNetworkContent, 2)
	for nt := range out {
		e := &testEndpoint{ip: nt.SrcIP, port: nt.SrcPort}
		k := e.String()
		actual[k] = append(actual[k], nt.Content)
	}

	expected := map[string][]akinet.ParsedNetworkContent{
		testEndpoint1.String(): []akinet.ParsedNetworkContent{
			akinet.AkitaPrince("hello"),
		},
		testEndpoint2.String(): []akinet.ParsedNetworkContent{
			akinet.RawBytes(memview.New([]byte("a|"))),
			akinet.AkitaPrince("bye"),
		},
	}
	if diff := netParseCmp(expected, actual); diff != "" {
		t.Errorf("reassembled data mismatch: %s", diff)
	}
}

func TestTCPOutofOrder(t *testing.T) {
	pkts := []gopacket.Packet{
		// Request packets. Expect to get "abc" because the reordering should be
		// handled by reassembly.
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("a"), 1),
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("c"), 3),
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("b"), 2),
		// Response packets. Expect to get "23" because we force accept packets
		// without seeing SYN, so after accepting seq 2 we are forced to drop seq 1.
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("2"), 2),
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("1"), 1),
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("3"), 3),
	}

	closeChan := make(chan struct{})
	defer close(closeChan)
	out, err := setupParseFromInterface(fakePcap(pkts), closeChan)
	if err != nil {
		t.Errorf("unexpected error setting up listener: %v", err)
		return
	}

	// Map src endpoint to bytes received for the flow.
	actual := make(map[string]string, 2)
	for nt := range out {
		if rbs, ok := nt.Content.(akinet.RawBytes); !ok {
			t.Errorf("returned content is not of type 'RawBytes', got %T", nt.Content)
			return
		} else {
			e := &testEndpoint{ip: nt.SrcIP, port: nt.SrcPort}
			k := e.String()
			actual[k] += rbs.String()
		}
	}

	expected := map[string]string{
		testEndpoint1.String(): "abc",
		testEndpoint2.String(): "23",
	}
	if diff := netParseCmp(expected, actual); diff != "" {
		t.Errorf("reassembled data mismatch: %s", diff)
	}
}

func TestTCPDuplicateAndOutofOrderSegments(t *testing.T) {
	pkts := []gopacket.Packet{
		// Request packets. Expect to get "abcd" because reassembly should handle
		// the the reordering and suppress duplicate.
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("a"), 1),
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("c"), 3),
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("c"), 3),
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("b"), 2),
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("d"), 4),
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("d"), 4),
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("c"), 3),
		CreatePacketWithSeq(ip1, ip2, port1, port2, []byte("b"), 2),
		// Response packets. Expect to get "23" because we force accept packets
		// without seeing SYN, so after accepting seq 2 we are forced to drop seq 1.
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("2"), 2),
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("1"), 1),
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("3"), 3),
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("1"), 1),
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("3"), 3),
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("3"), 3),
		CreatePacketWithSeq(ip2, ip1, port2, port1, []byte("1"), 1),
	}

	closeChan := make(chan struct{})
	defer close(closeChan)
	out, err := setupParseFromInterface(fakePcap(pkts), closeChan)
	if err != nil {
		t.Errorf("unexpected error setting up listener: %v", err)
		return
	}

	// Map src endpoint to bytes received for the flow.
	actual := make(map[string]string, 2)
	for nt := range out {
		if rbs, ok := nt.Content.(akinet.RawBytes); !ok {
			t.Errorf("returned content is not of type 'RawBytes', got %T", nt.Content)
			return
		} else {
			e := &testEndpoint{ip: nt.SrcIP, port: nt.SrcPort}
			k := e.String()
			actual[k] += rbs.String()
		}
	}

	expected := map[string]string{
		testEndpoint1.String(): "abcd",
		testEndpoint2.String(): "23",
	}
	if diff := netParseCmp(expected, actual); diff != "" {
		t.Errorf("reassembled data mismatch: %s", diff)
	}
}

// Test scenario where the underlying pcap channel closes before the opposite
// TCP flow appears. We should gracefully stop the parsers and close the
// output channel.
func TestPcapCloseBeforeTCPDuplexEvent(t *testing.T) {
	msgData := "721a0c00-93ea-4305-afba-e49cd910626b"
	msg := &testMessage{testEndpoint1, testEndpoint2, []byte(msgData)}
	// No packet in the opposite direction so the opposite flow is never
	// created.

	closeChan := make(chan struct{})
	defer close(closeChan)
	// fakePcap is going to close the pcap channel after sending all packets.
	pcap := fakePcap(makeTCPPackets(1, msg))
	out, err := setupParseFromInterface(pcap, closeChan)
	if err != nil {
		t.Fatalf("unexpected error setting up listener: %v", err)
	}

	var actual string
	for nt := range out {
		if b, ok := nt.Content.(akinet.RawBytes); !ok {
			t.Fatalf("returned content is not of type 'RawBytes', got %T", nt.Content)
		} else {
			actual += b.String()
		}
	}
	// The out channel should close itself once it detects that the underlying
	// pcap channel has closed.

	if diff := cmp.Diff(msgData, actual); diff != "" {
		t.Errorf("mismatch: %s", diff)
	}
}

// Test scenario where the underlying pcap channel hangs and we have to cancel
// the parsing channel.
func TestCancelHangingPcap(t *testing.T) {
	msgData := "c1e3aaab-df4c-4452-9761-9642960b0356"
	msg := &testMessage{testEndpoint1, testEndpoint2, []byte(msgData)}
	// No packet in the opposite direction so the opposite flow never shows up.

	closeChan := make(chan struct{})
	// forceCancelPcap is going to, well, hang.
	pcap := forceCancelPcap(makeTCPPackets(1, msg))
	out, err := setupParseFromInterface(pcap, closeChan)
	if err != nil {
		t.Fatalf("unexpected error setting up listener: %v", err)
	}

	var actual string
	for nt := range out {
		if b, ok := nt.Content.(akinet.RawBytes); !ok {
			t.Fatalf("returned content is not of type 'RawBytes', got %T", nt.Content)
		} else {
			actual += b.String()
		}

		if msgData == actual {
			// Manually cancel the parsing.
			close(closeChan)
		}
	}
	// The out channel should close itself once it detects that the underlying
	// pcap channel has closed.
}

func TestUDP(t *testing.T) {
	msgData := []byte("a7b40a05-ba12-4bee-bc48-033bdef70885")
	msg := &testMessage{testEndpoint1, testEndpoint2, msgData}

	closeChan := make(chan struct{})
	defer close(closeChan)
	pcap := fakePcap(makeUDPPackets(1, msg))
	out, err := setupParseFromInterface(pcap, closeChan)
	if err != nil {
		t.Fatalf("unexpected error setting up listener: %v", err)
	}

	var actual []akinet.ParsedNetworkTraffic
	for nt := range out {
		actual = append(actual, nt)
	}

	expected := []akinet.ParsedNetworkTraffic{
		{
			SrcIP:           ip1,
			SrcPort:         port1,
			DstIP:           ip2,
			DstPort:         port2,
			Content:         akinet.RawBytes(memview.New(msgData)),
			ObservationTime: testTime,
		},
	}

	if diff := netParseCmp(expected, actual); diff != "" {
		t.Errorf("mismatch: %s", diff)
	}
}
