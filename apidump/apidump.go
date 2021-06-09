package apidump

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/logrusorgru/aurora"
	"github.com/pkg/errors"
	"github.com/spf13/viper"

	"github.com/akitasoftware/akita-cli/location"
	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/tags"
)

// TODO(kku): make pcap timings more robust (e.g. inject a sentinel packet to
// mark start and end of pcap).
const (
	// Empirically, it takes 1s for pcap to be ready to process packets.
	// We budget for 5x to be safe.
	pcapStartWaitTime = 5 * time.Second

	// Empirically, it takes 1s for the first packet to become available for
	// processing.
	// We budget for 5x to be safe.
	pcapStopWaitTime = 5 * time.Second
)

const (
	subcommandOutputDelimiter = "======= _AKITA_SUBCOMMAND_ ======="
)

type Args struct {
	// Required args
	ClientID akid.ClientID
	Domain   string

	// Optional args

	// If both LocalPath and AkitaURI are set, data is teed to both local traces
	// and backend trace.
	// If unset, defaults to a random spec on Akita Cloud.
	Out location.Location

	Interfaces     []string
	Filter         string
	Tags           map[tags.Key]string
	PathExclusions []string
	HostExclusions []string

	// Rate-limiting parameters -- only one should be set to a non-default value.
	SampleRate         float64
	WitnessesPerMinute float64

	// If set, apidump will run the command in a subshell and terminate
	// automatically when the subcommand terminates.
	//
	// apidump will pipe stdout and stderr from the command. If the command stops
	// with non-zero exit code, apidump will also exit with the same exit code.
	ExecCommand string

	// Username to run ExecCommand as. If not set, defaults to the current user.
	ExecCommandUser string

	Plugins []plugin.AkitaPlugin
}

// DumpPacketCounters prints the accumulated packet counts per interface and per port,
// at Debug level, to stderr.  The first argument should be the keyed by interface names (as created
// in the Run function below); all we really need are those names.
func DumpPacketCounters(interfaces map[string]interfaceInfo, inboundSummary *trace.PacketCountSummary, outboundSummary *trace.PacketCountSummary) {
	// Using a map gives inconsistent order when iterating (even on the same run!)
	directions := []kgxapi.NetworkDirection{kgxapi.Inbound, kgxapi.Outbound}
	toReport := []*trace.PacketCountSummary{inboundSummary}
	if outboundSummary != nil {
		toReport = append(toReport, outboundSummary)
	}

	printer.Stderr.Debugf("==================================================\n")
	printer.Stderr.Debugf("Packets per interface:\n")
	printer.Stderr.Debugf("%15v %8v %7v %11v %5v\n", "", "", "TCP  ", "HTTP   ", "")
	printer.Stderr.Debugf("%15v %8v %7v %5v %5v %5v\n", "interface", "dir", "packets", "req", "resp", "unk")
	for n := range interfaces {
		for i, summary := range toReport {
			count := summary.TotalOnInterface(n)
			printer.Stderr.Debugf("%15s %8s %7d %5d %5d %5d\n",
				n,
				directions[i],
				count.TCPPackets,
				count.HTTPRequests,
				count.HTTPResponses,
				count.Unparsed,
			)
		}
	}

	printer.Stderr.Debugf("==================================================\n")
	printer.Stderr.Debugf("Packets per port:\n")
	printer.Stderr.Debugf("%8v %7v %11v %5v\n", "", "TCP  ", "HTTP   ", "")
	printer.Stderr.Debugf("%8v %7v %5v %5v %5v\n", "port", "packets", "req", "resp", "unk")
	for i, summary := range toReport {
		if directions[i] == kgxapi.Inbound {
			printer.Stderr.Debugf("--------- matching filter --------\n")
		} else {
			printer.Stderr.Debugf("------- not matching filter ------\n")
		}
		byPort := summary.AllPorts()
		// We don't really know what's in the BPF filter; we know every packet in inbound
		// must have matched, but that could be multiple ports, or some other criteria.
		for _, count := range byPort {
			printer.Stderr.Debugf("%8d %7d %5d %5d %5d\n",
				count.SrcPort,
				count.TCPPackets,
				count.HTTPRequests,
				count.HTTPResponses,
				count.Unparsed,
			)
		}
		if len(byPort) == 0 {
			printer.Stderr.Debugf("       no packets captured        \n")
		}
	}

	printer.Stderr.Debugf("==================================================\n")

}

// Captures packets from the network and adds them to a trace. The trace is
// created if it doesn't already exist.
//
// The args.Tags is expected to already contain information about how the trace
// is captured (e.g., whether the capture was user-initiated or is from CI, and
// any applicable information from CI).
func Run(args Args) error {
	// Get the interfaces to listen on.
	interfaces, err := getEligibleInterfaces(args.Interfaces)
	if err != nil {
		return errors.Wrap(err, "failed to list network interfaces")
	}

	// Build inbound and outbound filters for each interface.
	inboundFilters, outboundFilters, err := createBPFFilters(interfaces, args.Filter, 0)
	if err != nil {
		return err
	}
	printer.Debugln("Inbound BPF filters:", inboundFilters)
	printer.Debugln("Outbound BPF filters:", outboundFilters)

	// Build tags.
	traceTags := args.Tags
	if traceTags == nil {
		traceTags = map[tags.Key]string{}
	}
	// Store the current packet capture flags so we can reuse them in active
	// learning.
	if len(args.Interfaces) > 0 {
		traceTags[tags.XAkitaDumpInterfacesFlag] = strings.Join(args.Interfaces, ",")
	}
	if args.Filter != "" {
		traceTags[tags.XAkitaDumpFilterFlag] = args.Filter
	}

	// Build path filters.
	pathExclusions := make([]*regexp.Regexp, 0, len(args.PathExclusions))
	for _, f := range args.PathExclusions {
		if r, err := regexp.Compile(f); err != nil {
			return errors.Wrapf(err, "failed to compile path filter %q", f)
		} else {
			pathExclusions = append(pathExclusions, r)
		}
	}

	// Build host filters.
	hostExclusions := make([]*regexp.Regexp, 0, len(args.HostExclusions))
	for _, f := range args.HostExclusions {
		if r, err := regexp.Compile(f); err != nil {
			return errors.Wrapf(err, "failed to compile host filter %q", f)
		} else {
			hostExclusions = append(hostExclusions, r)
		}
	}

	// Validate args.Out and fill in any missing defaults.
	if uri := args.Out.AkitaURI; uri != nil {
		if uri.ObjectType == nil {
			uri.ObjectType = akiuri.TRACE.Ptr()
		} else if !uri.ObjectType.IsTrace() {
			return errors.Errorf("%q is not an Akita trace URI", uri)
		}

		// Use a random object name by default.
		if uri.ObjectName == "" {
			uri.ObjectName = util.RandomLearnSessionName()
		}
	}

	// If the output is targeted at the backend, create a shared backend
	// learn session.
	var backendSvc akid.ServiceID
	var backendLrn akid.LearnSessionID
	var learnClient rest.LearnClient
	if uri := args.Out.AkitaURI; uri != nil {
		frontClient := rest.NewFrontClient(args.Domain, args.ClientID)
		backendSvc, err = util.GetServiceIDByName(frontClient, uri.ServiceName)
		if err != nil {
			return err
		}
		learnClient = rest.NewLearnClient(args.Domain, args.ClientID, backendSvc)

		backendLrn, err = util.NewLearnSession(args.Domain, args.ClientID, backendSvc, uri.ObjectName, traceTags, nil)
		if err == nil {
			printer.Infof("Created new trace on Akita Cloud: %s\n", uri)
		} else {
			var httpErr rest.HTTPError
			if ok := errors.As(err, &httpErr); ok && httpErr.StatusCode == 409 {
				backendLrn, err = util.GetLearnSessionIDByName(learnClient, uri.ObjectName)
				if err != nil {
					return errors.Wrapf(err, "failed to lookup ID for existing trace %s", uri)
				}
				printer.Infof("Adding to existing trace: %s\n", uri)
			} else {
				return errors.Wrap(err, "failed to create or fetch trace already")
			}
		}
	}

	// Initialize packet counts
	inboundSummary := trace.NewPacketCountSummary()
	outboundSummary := trace.NewPacketCountSummary()

	// Initialized shared rate object, if we are configured with a rate limit
	var rateLimit *trace.SharedRateLimit
	if args.WitnessesPerMinute != 0.0 {
		rateLimit = trace.NewRateLimit(args.WitnessesPerMinute)
		defer rateLimit.Stop()
	}

	// Start collecting
	var doneWG sync.WaitGroup
	doneWG.Add(len(inboundFilters) + len(outboundFilters))
	errChan := make(chan error, len(inboundFilters)+len(outboundFilters)) // buffered enough so it never blocks
	stop := make(chan struct{})
	for _, dir := range []kgxapi.NetworkDirection{kgxapi.Inbound, kgxapi.Outbound} {
		var summary *trace.PacketCountSummary
		var filters map[string]string
		if dir == kgxapi.Inbound {
			filters = inboundFilters
			summary = inboundSummary
		} else {
			filters = outboundFilters
			summary = outboundSummary
		}

		for interfaceName, filter := range filters {
			var collector trace.Collector
			{
				var localCollector trace.Collector
				if args.Out.LocalPath != nil {
					if lc, err := createLocalCollector(interfaceName, *args.Out.LocalPath, dir, traceTags); err == nil {
						localCollector = lc
					} else {
						return err
					}
				}

				if args.Out.AkitaURI != nil && args.Out.LocalPath != nil {
					collector = trace.TeeCollector{
						Dst1: trace.NewBackendCollector(backendSvc, backendLrn, learnClient, dir, args.Plugins),
						Dst2: localCollector,
					}
				} else if args.Out.AkitaURI != nil {
					collector = trace.NewBackendCollector(backendSvc, backendLrn, learnClient, dir, args.Plugins)
				} else if args.Out.LocalPath != nil {
					collector = localCollector
				} else {
					return errors.Errorf("invalid output location")
				}
			}

			// Count packets that have *passed* filtering (so that we know whether the
			// trace is empty or not.)  In the future we could add columns for both
			// pre- and post-filtering.
			collector = &trace.PacketCountCollector{
				PacketCounts: summary,
				Collector:    collector,
			}

			if args.SampleRate != 1.0 {
				// This is a change from previous behavior: now we sample after filtering
				// instead of before.
				collector = &trace.SamplingCollector{
					SampleRate: args.SampleRate,
					Collector:  collector,
				}
			}
			if rateLimit != nil {
				collector = rateLimit.NewCollector(collector)
			}

			// Add filters
			collector = &trace.UserTrafficCollector{
				Collector: trace.NewHTTPPathFilterCollector(
					pathExclusions,
					trace.NewHTTPHostFilterCollector(hostExclusions, collector),
				),
			}

			go func(interfaceName, filter string) {
				defer doneWG.Done()
				// Collect trace. This blocks until stop is closed or an error occurs.
				if err := trace.Collect(stop, interfaceName, filter, collector, summary); err != nil {
					errChan <- errors.Wrapf(err, "failed to collect trace on interface %s", interfaceName)
				}
			}(interfaceName, filter)
		}
	}

	{
		iNames := make([]string, 0, len(interfaces))
		for n := range interfaces {
			iNames = append(iNames, n)
		}
		printer.Stderr.Infof("Running learn mode on interfaces %s\n", strings.Join(iNames, ", "))
	}
	if len(outboundFilters) == 0 {
		printer.Stderr.Warningf("%s\n", aurora.Yellow("--filter flag is not set, this means that:"))
		printer.Stderr.Warningf("%s\n", aurora.Yellow("  - all network traffic is treated as your API traffic"))
		printer.Stderr.Warningf("%s\n", aurora.Yellow("  - outbound witness collection is disabled"))
	}

	var stopErr error
	if args.ExecCommand != "" {
		printer.Stderr.Infof("Running subcommand...\n\n\n")

		time.Sleep(pcapStartWaitTime)

		// Print delimiter so it's easier to differentiate subcommand output from
		// Akita output.
		fmt.Fprintln(os.Stdout, subcommandOutputDelimiter)
		fmt.Fprintln(os.Stderr, subcommandOutputDelimiter)
		cmdErr := runCommand(args.ExecCommandUser, args.ExecCommand)
		fmt.Fprintln(os.Stdout, subcommandOutputDelimiter)
		fmt.Fprintln(os.Stderr, subcommandOutputDelimiter)

		if cmdErr != nil {
			stopErr = errors.Wrap(cmdErr, "failed to run subcommand")
			// We promised to preserve the subcommand's exit code.
			// Explicitly notify whoever is running us to exit.
			if exitErr, ok := errors.Cause(stopErr).(*exec.ExitError); ok {
				stopErr = util.ExitError{
					ExitCode: exitErr.ExitCode(),
					Err:      stopErr,
				}
			}
		} else {
			// Check if we have any errors on our side.
			select {
			case err := <-errChan:
				stopErr = err
				printer.Stderr.Errorf("Encountered error while collecting traces, stopping...\n")
			default:
				printer.Stderr.Infof("Subcommand finished successfully, stopping trace collection...\n")
			}
		}
	} else {
		// Don't sleep pcapStartWaitTime in interactive mode since the user can send
		// SIGINT while we're sleeping too and sleeping introduces visible lag.
		printer.Stderr.Infof("Send SIGINT (Ctrl-C) to stop...\n")

		// Set up signal handler to stop packet processors on SIGINT or when one of
		// the processors returns an error.
		{
			// Must use buffered channel for signals since the signal package does not
			// block when sending signals.
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt)
			select {
			case <-sig:
				printer.Stderr.Infof("Received SIGINT, stopping trace collection...\n")
			case err := <-errChan:
				stopErr = err
				printer.Stderr.Errorf("Encountered error while collecting traces, stopping...\n")
			}
		}
	}

	time.Sleep(pcapStopWaitTime)

	// Signal all processors to stop.
	close(stop)

	// Wait for processors to exit.
	doneWG.Wait()
	if stopErr != nil {
		return errors.Wrap(stopErr, "trace collection failed")
	}

	if viper.GetBool("debug") {
		if len(outboundFilters) == 0 {
			DumpPacketCounters(interfaces, inboundSummary, nil)
		} else {
			DumpPacketCounters(interfaces, inboundSummary, outboundSummary)
		}
	}

	// Check summary to see if the inbound trace will have anything in it.
	totalCount := inboundSummary.Total()
	if totalCount.HTTPRequests == 0 && totalCount.HTTPResponses == 0 {
		// TODO: recognize TLS handshakes and count them separately!
		if totalCount.TCPPackets == 0 {
			if outboundSummary.Total().TCPPackets == 0 {
				printer.Stderr.Infof("Did not capture any TCP packets during the trace.\n")
				printer.Stderr.Infof("%s\n", aurora.Yellow("This may mean the traffic is on a different interface, or that"))
				printer.Stderr.Infof("%s\n", aurora.Yellow("there is a problem sending traffic to the API."))
			} else {
				printer.Stderr.Infof("Did not capture any TCP packets matching the filter.\n")
				printer.Stderr.Infof("%s\n", aurora.Yellow("This may mean your filter is incorrect, such as the wrong TCP port."))
			}
		} else if totalCount.Unparsed > 0 {
			printer.Stderr.Infof("Captured %d TCP packets total; %d unparsed TCP segments.\n",
				totalCount.TCPPackets, totalCount.Unparsed)
			printer.Stderr.Infof("%s\n", aurora.Yellow("This may mean you are trying to capture HTTPS traffic."))
			printer.Stderr.Infof("See https://docs.akita.software/docs/proxy-for-encrypted-traffic\n")
			printer.Stderr.Infof("for instructions on using a proxy, or generate a HAR file with\n")
			printer.Stderr.Infof("your browser as described in\n")
			printer.Stderr.Infof("https://docs.akita.software/docs/collect-client-side-traffic-2\n")
		}
		printer.Stderr.Errorf("%s 🛑\n\n", aurora.Red("No inbound HTTP calls captured!"))
		return errors.New("incoming API trace is empty")
	}
	if totalCount.HTTPRequests == 0 {
		printer.Stderr.Warningf("%s ⚠\n\n", aurora.Yellow("Saw HTTP responses, but not requests."))
		return nil
	}
	if totalCount.HTTPResponses == 0 {
		printer.Stderr.Warningf("%s ⚠\n\n", aurora.Yellow("Saw HTTP requests, but not responses."))
		return nil
	}

	printer.Stderr.Infof("%s 🎉\n\n", aurora.Green("Success!"))
	return nil
}

func createLocalCollector(interfaceName, outDir string, netDir kgxapi.NetworkDirection, tags map[tags.Key]string) (trace.Collector, error) {
	if fi, err := os.Stat(outDir); err == nil {
		// File exists, check if it's a directory.
		if !fi.IsDir() {
			return nil, errors.Errorf("%s is not a directory", outDir)
		}

		// Check if we have permission to write to the directory.
		testFile := filepath.Join(outDir, "akita_test")
		if err := ioutil.WriteFile(testFile, []byte{1}, 0644); err == nil {
			os.Remove(testFile)
		} else {
			return nil, errors.Wrapf(err, "cannot access directory %s", outDir)
		}
	} else {
		// Attempt to create one to make sure there's no permission problem.
		if err := os.Mkdir(outDir, 0755); err != nil {
			return nil, errors.Wrapf(err, "failed to create directory %s", outDir)
		}
	}

	return trace.NewHARCollector(interfaceName, outDir, netDir == kgxapi.Outbound, tags), nil
}
