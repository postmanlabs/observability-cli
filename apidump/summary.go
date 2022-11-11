package apidump

import (
	"fmt"
	"sort"

	"github.com/akitasoftware/akita-libs/client_telemetry"
	"github.com/akitasoftware/go-utils/math"
	"github.com/spf13/viper"

	"github.com/akitasoftware/akita-cli/pcap"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/trace"
)

// Captures apidump progress.
type Summary struct {
	// Indicates whether this summary includes information about packets that did
	// not meet the BPF filters specified by the user.
	CapturingNegation bool

	Interfaces      map[string]interfaceInfo
	NegationFilters map[string]string
	NumUserFilters  int

	// Values that change over the course of apidump are pointers.
	FilterSummary    *trace.PacketCounter
	PrefilterSummary *trace.PacketCounter
	NegationSummary  *trace.PacketCounter
}

func NewSummary(
	capturingNegation bool,
	interfaces map[string]interfaceInfo,
	negationFilters map[string]string,
	numUserFilters int,
	filterSummary *trace.PacketCounter,
	prefilterSummary *trace.PacketCounter,
	negationSummary *trace.PacketCounter,
) *Summary {
	return &Summary{
		CapturingNegation: capturingNegation,
		Interfaces:        interfaces,
		NegationFilters:   negationFilters,
		NumUserFilters:    numUserFilters,
		FilterSummary:     filterSummary,
		PrefilterSummary:  prefilterSummary,
		NegationSummary:   negationSummary,
	}
}

// Dumps packet counters for packets captured and sent to the Akita backend.
// If the debug flag is set, also prints packets taht were captured but not
// sent to the backend.
func (s *Summary) PrintPacketCounts() {
	s.PrintPacketCountHighlights()

	if viper.GetBool("debug") {
		if len(s.NegationFilters) == 0 {
			DumpPacketCounters(printer.Stderr.Infof, s.Interfaces, s.FilterSummary, nil, true)
		} else {
			DumpPacketCounters(printer.Stderr.Infof, s.Interfaces, s.FilterSummary, s.NegationSummary, true)
		}
		if s.NumUserFilters > 0 {
			printer.Stderr.Debugf("+++ Counts before allow and exclude filters and sampling +++\n")
			DumpPacketCounters(printer.Stderr.Debugf, s.Interfaces, s.PrefilterSummary, nil, false)
		}
	}
}

// Summarize the top sources of traffic seen in a log-friendly format.
// This appears before PrintWarnings, and should highlight the raw data.
//
// TODO: it would be nice to show hostnames if we have them? To more clearly
// identify the traffic.
func (s *Summary) PrintPacketCountHighlights() {
	summaryLimit := 20
	top := s.FilterSummary.Summary(summaryLimit)

	totalTraffic := top.Total.TCPPackets
	if totalTraffic == 0 {
		// PrintWarnings already covers this case
		return
	}

	// If we hit the limit of the number of ports we tracked, mention so.
	// This should (hopefully) be unlikely.
	if top.ByPortOverflow != nil {
		printer.Stderr.Infof(
			"More than %d ports with traffic.  Showing the top %d of the first %d.\n",
			top.ByPortOverflowLimit, math.Min(summaryLimit, top.ByPortOverflowLimit), top.ByPortOverflowLimit,
		)
	}

	// Sort by TCP traffic volume and list in descending order.
	// This is already sorted in TopN but that ordering
	// doesn't seem accessible here.
	ports := make([]int, 0, len(top.TopByPort))
	for p := range top.TopByPort {
		ports = append(ports, p)
	}
	sort.Slice(ports, func(i, j int) bool {
		return top.TopByPort[ports[i]].TCPPackets > top.TopByPort[ports[j]].TCPPackets
	})

	totalListedForPorts := 0
	for i, p := range ports {
		thisPort := top.TopByPort[p]
		pct := thisPort.TCPPackets * 100 / totalTraffic
		totalListedForPorts += thisPort.TCPPackets

		// Stop when the running total would be >100%.  (Each packet is counted both
		// in the source port and in the destination port; we want to avoid
		// showing a bunch of ephemeral ports even if they're all above the threshold.)
		//
		// Before that limit is hit, list at least two sources, but stop when less than 3% of traffic.
		if (totalListedForPorts > totalTraffic) || (pct < 3 && i >= 2) {
			break
		}

		printSummaryLine(fmt.Sprintf("TCP port %5d", p), *thisPort, s.PrefilterSummary.TotalOnPort(p), pct, true)
	}

	// Sort by HTTP traffic volume, then TLS handshake counts, both descending.
	// We do not have TCP packet counts for hosts.
	hosts := make([]string, 0, len(top.TopByHost))
	totalCountForHosts := 0
	for h, c := range top.TopByHost {
		hosts = append(hosts, h)
		totalCountForHosts += c.HTTPRequests + c.HTTPResponses + c.TLSHello
	}
	sort.Slice(hosts, func(i, j int) bool {
		left := top.TopByHost[hosts[i]]
		right := top.TopByHost[hosts[j]]

		if left.HTTPRequests != right.HTTPRequests {
			return left.HTTPRequests > right.HTTPRequests
		} else if left.HTTPResponses != right.HTTPResponses {
			return left.HTTPResponses > right.HTTPResponses
		} else {
			return left.TLSHello > right.TLSHello
		}
	})

	// Take the first N hosts capturing at least 97% of the data.  This avoids
	// a long tail of hosts with very few TLS handshakes.
	printUpTo := 0
	longestHostLength := 0
	countSoFar := 0
	for i, h := range hosts {
		thisHost := top.TopByHost[h]
		countSoFar += thisHost.HTTPRequests + thisHost.HTTPResponses + thisHost.TLSHello
		pctSoFar := countSoFar * 100 / totalCountForHosts

		if 97 < pctSoFar && i >= 2 {
			break
		}

		printUpTo = i + 1
		longestHostLength = math.Max(longestHostLength, len(h))
	}

	for _, h := range hosts[:printUpTo] {
		thisHost := top.TopByHost[h]
		printSummaryLine(fmt.Sprintf("Host %-*s", longestHostLength, h), *thisHost, s.PrefilterSummary.TotalOnHost(h), 0 /* unused */, false)
	}
}

func printSummaryLine(label string, data, preFilter client_telemetry.PacketCounts, pct int, includeTCP bool) {
	tcpMsg := ""
	if includeTCP {
		tcpMsg = fmt.Sprintf("%5d packets (%d%% of total), ", data.TCPPackets, pct)
	}

	// If we saw any HTTP traffic, report that.  But, if there's a high percentage of unparsed packets, note that too.
	if data.HTTPRequests+data.HTTPResponses > 0 {
		printer.Stderr.Infof("%s %s%d HTTP requests, %d HTTP responses, %d TLS handshakes, %d unparsed packets.\n",
			label, tcpMsg, data.HTTPRequests, data.HTTPResponses, data.TLSHello, data.Unparsed)
		if data.TLSHello > 0 {
			printer.Stderr.Infof("%s appears to contain a mix of encrypted and unencrypted traffic.\n", label)
		} else if data.Unparsed > data.TCPPackets*3/10 {
			printer.Stderr.Infof("%s has an unusually high amount of traffic that Akita cannot parse.\n", label)
		}
		if data.HTTP2Prefaces > 0 {
			printer.Stderr.Infof("%s Contains HTTP/2 traffic (%d connections detected), which Akita cannot parse.\n", label, data.HTTP2Prefaces)
		}
		return
	}

	// If we saw HTTP traffic but it was filtered, give the pre-filter statistics
	if preFilter.HTTPRequests+preFilter.HTTPResponses > 0 {
		printer.Stderr.Infof("%s %sno HTTP requests or responses satisfied all the filters you gave, but %d HTTP requests and %d HTTP responses were seen before your path and host filters were applied.\n",
			label, tcpMsg, preFilter.HTTPRequests, preFilter.HTTPResponses)
		return
	}

	// If we saw TLS, report the presence of encrypted traffic
	if data.TLSHello > 0 {
		printer.Stderr.Infof("%s %sno HTTP requests or responses, %d TLS handshakes indicating encrypted traffic.\n",
			label, tcpMsg, data.TLSHello)
		return
	}

	// If we saw HTTP/2, report it.
	if data.HTTP2Prefaces > 0 {
		printer.Stderr.Infof("%s %sno HTTP/1.1 requests or responses, %d HTTP/2 connection attempts. Akita cannot currently parse HTTP/2.\n",
			label, tcpMsg, data.HTTP2Prefaces)
		return
	}

	// Flag as unparsable
	printer.Stderr.Infof("%s %sno HTTP requests or responses; the data to this service could not be parsed.\n",
		label, tcpMsg)
}

// Prints warnings based on packet capture behavior, such as not capturing
// any packets, capturing packets but failing to parse them, etc.
func (s *Summary) PrintWarnings() {
	// Report on recoverable error counts during trace
	if pcap.CountNilAssemblerContext > 0 || pcap.CountNilAssemblerContextAfterParse > 0 || pcap.CountBadAssemblerContextType > 0 {
		printer.Stderr.Infof("Detected packet assembly context problems during capture: %v empty, %v bad type, %v empty after parse. ",
			pcap.CountNilAssemblerContext,
			pcap.CountBadAssemblerContextType,
			pcap.CountNilAssemblerContextAfterParse)
		printer.Stderr.Infof("These errors may cause some packets to be missing from the trace.\n")
	}

	// Check summary to see if the trace will have anything in it.
	totalCount := s.FilterSummary.Total()
	if totalCount.HTTPRequests == 0 && totalCount.HTTPResponses == 0 {
		if totalCount.TCPPackets == 0 {
			if s.CapturingNegation && s.NegationSummary.Total().TCPPackets == 0 {
				msg := "Did not capture any TCP packets during the trace. " +
					"This may mean the traffic is on a different interface, or that " +
					"there is a problem sending traffic to the API."
				printer.Stderr.Infof("%s\n", printer.Color.Yellow(msg))
			} else {
				msg := "Did not capture any TCP packets matching the filter. " +
					"This may mean your filter is incorrect, such as the wrong TCP port."
				printer.Stderr.Infof("%s\n", printer.Color.Yellow(msg))
			}
		} else if totalCount.TLSHello > 0 {
			msg := fmt.Sprintf("Captured %d TLS handshake messages out of %d total TCP segments. ", totalCount.TLSHello, totalCount.TCPPackets) +
				"This may mean you are trying to capture HTTPS traffic, which is currently unsupported."
			printer.Stderr.Infof("%s\n", msg)
		} else if totalCount.Unparsed > 0 {
			msg := fmt.Sprintf("Captured %d TCP packets total; %d unparsed TCP segments. ", totalCount.TCPPackets, totalCount.Unparsed) +
				"No TLS headers were found, so this may represent a network protocol that the agent does not know how to parse."
			printer.Stderr.Infof("%s\n", msg)
		} else if s.NumUserFilters > 0 && s.PrefilterSummary.Total().HTTPRequests != 0 {
			printer.Stderr.Infof("Captured %d HTTP requests before allow and exclude rules, but all were filtered.\n",
				s.PrefilterSummary.Total().HTTPRequests)
		}
		printer.Stderr.Errorf("%s 🛑\n\n", printer.Color.Red("No HTTP calls captured!"))
		return
	}
	if totalCount.HTTPRequests == 0 {
		printer.Stderr.Warningf("%s ⚠\n\n", printer.Color.Yellow("Saw HTTP responses, but not requests."))
	}
	if totalCount.HTTPResponses == 0 {
		printer.Stderr.Warningf("%s ⚠\n\n", printer.Color.Yellow("Saw HTTP requests, but not responses."))
	}
}

// Returns true if the trace generated from this apidump will be empty.
func (s *Summary) IsEmpty() bool {
	// Check summary to see if the trace will have anything in it.
	totalCount := s.FilterSummary.Total()
	return totalCount.HTTPRequests == 0 && totalCount.HTTPResponses == 0
}

// DumpPacketCounters prints the accumulated packet counts per interface and per port,
// to the logging function specified in the first argument.
// The "interfaces" argument should be the map keyed by interface names (as created
// in the apidump.Run function); all we really need are those names.
func DumpPacketCounters(logf func(f string, args ...interface{}), interfaces map[string]interfaceInfo, matchedSummary *trace.PacketCounter, unmatchedSummary *trace.PacketCounter, showInterface bool) {
	// Using a map gives inconsistent order when iterating (even on the same run!)
	filterStates := []filterState{matchedFilter, notMatchedFilter}
	toReport := []*trace.PacketCounter{matchedSummary}
	if unmatchedSummary != nil {
		toReport = append(toReport, unmatchedSummary)
	}

	if showInterface {
		logf("========================================================\n")
		logf("Packets per interface:\n")
		logf("%15v %9v %7v %11v %5v %5v\n", "", "", "TCP  ", "HTTP   ", "TLS  ", "")
		logf("%15v %9v %7v %5v %5v %5v %5v\n", "interface", "dir", "packets", "req", "resp", "hello", "unk")
		for n := range interfaces {
			for i, summary := range toReport {
				count := summary.TotalOnInterface(n)
				logf("%15s %9s %7d %5d %5d %5d %5d\n",
					n,
					filterStates[i],
					count.TCPPackets,
					count.HTTPRequests,
					count.HTTPResponses,
					count.TLSHello,
					count.Unparsed,
				)
			}
		}
	}

	logf("========================================================\n")
	logf("Packets per port:\n")
	logf("%8v %7v %11v %5v %5v\n", "", "TCP  ", "HTTP   ", "TLS  ", "")
	logf("%8v %7v %5v %5v %5v %5v\n", "port", "packets", "req", "resp", "hello", "unk")
	for i, summary := range toReport {
		if filterStates[i] == matchedFilter {
			logf("----------- matching filter ------------\n")
		} else {
			logf("--------- not matching filter ----------\n")
		}
		byPort := summary.AllPorts()
		// We don't really know what's in the BPF filter; we know every packet in
		// matchedSummary must have matched, but that could be multiple ports, or
		// some other criteria.
		for _, count := range byPort {
			logf("%8d %7d %5d %5d %5d %5d\n",
				count.SrcPort,
				count.TCPPackets,
				count.HTTPRequests,
				count.HTTPResponses,
				count.TLSHello,
				count.Unparsed,
			)
		}
		if len(byPort) == 0 {
			logf("       no packets captured        \n")
		}
	}

	logf("==================================================\n")

}
