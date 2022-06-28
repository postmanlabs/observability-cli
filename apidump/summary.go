package apidump

import (
	"github.com/akitasoftware/akita-cli/pcap"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/spf13/viper"
)

// Captures apidump progress.
type Summary struct {
	CapturingNegation bool
	Interfaces        map[string]interfaceInfo
	NegationFilters   map[string]string
	NumUserFilters    int

	// Values that change over the course of apidump are pointers.
	FilterSummary    *trace.PacketCountSummary
	PrefilterSummary *trace.PacketCountSummary
	NegationSummary  *trace.PacketCountSummary
}

func NewSummary(
	capturingNegation bool,
	interfaces map[string]interfaceInfo,
	negationFilters map[string]string,
	numUserFilters int,
	filterSummary *trace.PacketCountSummary,
	prefilterSummary *trace.PacketCountSummary,
	negationSummary *trace.PacketCountSummary,
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
	if len(s.NegationFilters) == 0 {
		DumpPacketCounters(printer.Stderr.Infof, s.Interfaces, s.FilterSummary, nil, true)
	} else {
		DumpPacketCounters(printer.Stderr.Infof, s.Interfaces, s.FilterSummary, s.NegationSummary, true)
	}

	if viper.GetBool("debug") {
		if s.NumUserFilters > 0 {
			printer.Stderr.Debugf("+++ Counts before allow and exclude filters and sampling +++\n")
			DumpPacketCounters(printer.Stderr.Debugf, s.Interfaces, s.PrefilterSummary, nil, false)
		}
	}
}

// Prints warnings based on packet capture behavior, such as not capturing
// any packets, capturing packets but failing to parse them, etc.
func (s *Summary) PrintWarnings() {
	// Report on recoverable error counts during trace
	if pcap.CountNilAssemblerContext > 0 || pcap.CountNilAssemblerContextAfterParse > 0 || pcap.CountBadAssemblerContextType > 0 {
		printer.Stderr.Infof("Detected packet assembly context problems during capture: %v empty, %v bad type, %v empty after parse",
			pcap.CountNilAssemblerContext,
			pcap.CountBadAssemblerContextType,
			pcap.CountNilAssemblerContextAfterParse)
		printer.Stderr.Infof("These errors may cause some packets to be missing from the trace.")
	}

	// Check summary to see if the trace will have anything in it.
	totalCount := s.FilterSummary.Total()
	if totalCount.HTTPRequests == 0 && totalCount.HTTPResponses == 0 {
		// TODO: recognize TLS handshakes and count them separately!
		if totalCount.TCPPackets == 0 {
			if s.CapturingNegation && s.NegationSummary.Total().TCPPackets == 0 {
				printer.Stderr.Infof("%s\n", printer.Color.Yellow("Did not capture any TCP packets during the trace."))
				printer.Stderr.Infof("%s\n", printer.Color.Yellow("This may mean the traffic is on a different interface, or that"))
				printer.Stderr.Infof("%s\n", printer.Color.Yellow("there is a problem sending traffic to the API."))
			} else {
				printer.Stderr.Infof("%s\n", printer.Color.Yellow("Did not capture any TCP packets matching the filter."))
				printer.Stderr.Infof("%s\n", printer.Color.Yellow("This may mean your filter is incorrect, such as the wrong TCP port."))
			}
		} else if totalCount.Unparsed > 0 {
			printer.Stderr.Infof("Captured %d TCP packets total; %d unparsed TCP segments.\n",
				totalCount.TCPPackets, totalCount.Unparsed)
			printer.Stderr.Infof("%s\n", printer.Color.Yellow("This may mean you are trying to capture HTTPS traffic."))
			printer.Stderr.Infof("See https://docs.akita.software/docs/proxy-for-encrypted-traffic\n")
			printer.Stderr.Infof("for instructions on using a proxy, or generate a HAR file with\n")
			printer.Stderr.Infof("your browser as described in\n")
			printer.Stderr.Infof("https://docs.akita.software/docs/collect-client-side-traffic-2\n")
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
// at Debug level, to stderr.  The first argument should be the keyed by interface names (as created
// in the Run function below); all we really need are those names.
func DumpPacketCounters(logf func(f string, args ...interface{}), interfaces map[string]interfaceInfo, matchedSummary *trace.PacketCountSummary, unmatchedSummary *trace.PacketCountSummary, showInterface bool) {
	// Using a map gives inconsistent order when iterating (even on the same run!)
	filterStates := []filterState{matchedFilter, notMatchedFilter}
	toReport := []*trace.PacketCountSummary{matchedSummary}
	if unmatchedSummary != nil {
		toReport = append(toReport, unmatchedSummary)
	}

	if showInterface {
		logf("==================================================\n")
		logf("Packets per interface:\n")
		logf("%15v %8v %7v %11v %5v\n", "", "", "TCP  ", "HTTP   ", "")
		logf("%15v %8v %7v %5v %5v %5v\n", "interface", "dir", "packets", "req", "resp", "unk")
		for n := range interfaces {
			for i, summary := range toReport {
				count := summary.TotalOnInterface(n)
				logf("%15s %9s %7d %5d %5d %5d\n",
					n,
					filterStates[i],
					count.TCPPackets,
					count.HTTPRequests,
					count.HTTPResponses,
					count.Unparsed,
				)
			}
		}
	}

	logf("==================================================\n")
	logf("Packets per port:\n")
	logf("%8v %7v %11v %5v\n", "", "TCP  ", "HTTP   ", "")
	logf("%8v %7v %5v %5v %5v\n", "port", "packets", "req", "resp", "unk")
	for i, summary := range toReport {
		if filterStates[i] == matchedFilter {
			logf("--------- matching filter --------\n")
		} else {
			logf("------- not matching filter ------\n")
		}
		byPort := summary.AllPorts()
		// We don't really know what's in the BPF filter; we know every packet in
		// matchedSummary must have matched, but that could be multiple ports, or
		// some other criteria.
		for _, count := range byPort {
			logf("%8d %7d %5d %5d %5d\n",
				count.SrcPort,
				count.TCPPackets,
				count.HTTPRequests,
				count.HTTPResponses,
				count.Unparsed,
			)
		}
		if len(byPort) == 0 {
			logf("       no packets captured        \n")
		}
	}

	logf("==================================================\n")

}
