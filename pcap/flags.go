package pcap

import (
	"time"

	flag "github.com/spf13/pflag"
)

var (
	// TODO: packet capture timeouts are tuned using the packet capture tests on a
	// particular developer's machine. We should implement some autotuning logic
	// based on specific machine that the broker is running on.
	// https://app.clubhouse.io/akita-software/story/521
	PktCaptureWaitFlag         = flag.Duration("packet_capture_wait_duration", 30*time.Millisecond, "Amount of time to wait after API calls complete before stopping packet capture. This has a direct impact on our API test throughput.")
	parserSelectionTimeoutFlag = flag.Duration("tcp_parser_selection_timeout", 5*time.Millisecond, "How much time to spend peeking at both sides of a TCP stream to determine which parser to use. Should be lower than --packet_capture_wait_duration to avoid stopping packet capture before data is processed by a parser.")
)

func init() {
	flag.CommandLine.MarkHidden("packet_capture_wait_duration")
	flag.CommandLine.MarkHidden("tcp_parser_selection_timeout")
}
