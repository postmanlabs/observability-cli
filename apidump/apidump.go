package apidump

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/pkg/errors"
	"github.com/spf13/viper"

	"github.com/akitasoftware/akita-cli/ci"
	"github.com/akitasoftware/akita-cli/deployment"
	"github.com/akitasoftware/akita-cli/location"
	"github.com/akitasoftware/akita-cli/pcap"
	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/tcp_conn_tracker"
	"github.com/akitasoftware/akita-cli/tls_conn_tracker"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
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

	// Number of top ports to show in telemetry
	topNForSummary = 10

	// Context timeout for telemetry upload
	telemetryTimeout = 30 * time.Second
)

const (
	subcommandOutputDelimiter = "======= _AKITA_SUBCOMMAND_ ======="
)

type filterState string

const (
	matchedFilter    filterState = "MATCHED"
	notMatchedFilter filterState = "UNMATCHED"
)

type Args struct {
	// Required args
	ClientID akid.ClientID
	Domain   string

	// Optional args

	// If both LocalPath and AkitaURI are set, data is teed to both local traces
	// and backend trace.
	// If unset, defaults to a random spec name on Akita Cloud.
	Out location.Location

	Interfaces     []string
	Filter         string
	Tags           map[tags.Key]string
	PathExclusions []string
	HostExclusions []string
	PathAllowlist  []string
	HostAllowlist  []string

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

	// How often to rotate learn sessions; set to zero to disable rotation.
	LearnSessionLifetime time.Duration

	// Deployment tag value; may come from an environment variable
	Deployment string

	// Print packet capture statistics after N seconds.
	StatsLogDelay int

	// Periodically report telemetry every N seconds thereafter
	TelemetryInterval int
}

func (args *Args) lint() {
	// Modifies the input to remove empty strings. Returns true if the input was
	// modified.
	removeEmptyStrings := func(strings []string) ([]string, bool) {
		i := 0
		modified := false
		for _, elt := range strings {
			if len(elt) > 0 {
				strings[i] = elt
				i++
			} else {
				modified = true
			}
		}
		strings = strings[:i]
		return strings, modified
	}

	// Empty path/host-exclusion regular expressions will exclude everything.
	// Ignore these and print a warning.
	for paramName, argsPtr := range map[string]*[]string{
		"--path-exclusions": &args.PathExclusions,
		"--host-exclusions": &args.HostExclusions,
	} {
		modified := false
		*argsPtr, modified = removeEmptyStrings(*argsPtr)
		if modified {
			printer.Stderr.Warningf("Ignoring empty regex in %s, which would otherwise exclude everything\n", paramName)
		}
	}

	// Empty path/host-inclusion regular expressions will include everything. If
	// there are any non-empty regular expressions, ignore the empty regexes and
	// print a warning.
	for paramName, argsPtr := range map[string]*[]string{
		"--path-allow": &args.PathAllowlist,
		"--host-allow": &args.HostAllowlist,
	} {
		modified := false
		*argsPtr, modified = removeEmptyStrings(*argsPtr)
		if modified && len(*argsPtr) > 0 {
			printer.Stderr.Warningf("Ignoring empty regex in %s, which would otherwise include everything\n", paramName)
		}
	}
}

// args.Tags may be initialized via the command line, but automated settings
// are mainly performed here (for now.)
func collectTraceTags(args *Args) map[tags.Key]string {
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

	// Set CI type and tags on trace
	ciType, _, ciTags := ci.GetCIInfo()
	if ciType != ci.Unknown {
		for k, v := range ciTags {
			traceTags[k] = v
		}
		traceTags[tags.XAkitaSource] = tags.CISource
	}

	// Import information about production or staging environment
	if args.Deployment != "" {
		traceTags[tags.XAkitaDeployment] = args.Deployment
		traceTags[tags.XAkitaSource] = tags.DeploymentSource
	}
	deployment.UpdateTags(traceTags)

	// Set source to user by default (if not CI or deployment)
	if _, ok := traceTags[tags.XAkitaSource]; !ok {
		traceTags[tags.XAkitaSource] = tags.UserSource
	}

	printer.Debugln("trace tags:", traceTags)
	return traceTags
}

func compileRegexps(filters []string, name string) ([]*regexp.Regexp, error) {
	result := make([]*regexp.Regexp, len(filters))
	for i, f := range filters {
		r, err := regexp.Compile(f)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to compile %s %q", name, f)
		}
		result[i] = r
	}
	return result, nil
}

// Periodically create a new learn session with a random name.
func RotateLearnSession(args *Args, done <-chan struct{}, collectors []trace.LearnSessionCollector, learnClient rest.LearnClient, backendSvc akid.ServiceID, traceTags map[tags.Key]string) {
	t := time.NewTicker(args.LearnSessionLifetime)
	defer t.Stop()

	for {
		select {
		case <-done:
			return

		case <-t.C:
			traceName := util.RandomLearnSessionName()
			backendLrn, err := util.NewLearnSession(args.Domain, args.ClientID, backendSvc, traceName, traceTags, nil)
			if err != nil {
				printer.Errorf("Failed to create trace %s: %v\n", traceName, err)
				break
			}
			printer.Infof("Rotating to new trace on Akita Cloud: %v\n", traceName)
			for _, c := range collectors {
				c.SwitchLearnSession(backendLrn)
			}
		}
	}
}

// Goroutine to send telemetry, stop when "done" is closed.
//
// Prints a summary after a short delay.  This ensures that statistics will
// appear in customer logs close to when the process is started.
// Omits if args.StatsLogDelay is <= 0.
//
// Sends telemetry to the server on a regular basis.
// Omits if args.TelemetryInterval is <= 0
func SendTelemetry(args *Args, learnClient rest.LearnClient, backendSvc akid.ServiceID, dumpSummary *Summary, done <-chan struct{}) {
	if args.StatsLogDelay <= 0 && args.TelemetryInterval <= 0 {
		return
	}

	req := kgxapi.PostClientPacketCaptureStatsRequest{
		ClientID:                  args.ClientID,
		ObservedStartingAt:        time.Now().UTC(),
		ObservedDurationInSeconds: args.StatsLogDelay,
	}

	// Upload "req" to the server.
	send := func() {
		req.PacketCountSummary = dumpSummary.FilterSummary.Summary(topNForSummary)
		ctx, cancel := context.WithTimeout(context.Background(), telemetryTimeout)
		defer cancel()
		err := learnClient.PostClientPacketCaptureStats(ctx, backendSvc, args.Deployment, req)
		if err != nil {
			// Log an error and continue.
			printer.Stderr.Errorf("Failed to send telemetry statistics: %s", err)
		}
	}

	// Send telemetry start event.  Stats are nil.
	send()

	if args.StatsLogDelay > 0 {
		// Wait while capturing statistics.
		time.Sleep(time.Duration(args.StatsLogDelay) * time.Second)

		// Print telemetry data.
		printer.Stderr.Infof("Printing packet capture statistics after %d seconds of capture.\n", args.StatsLogDelay)
		dumpSummary.PrintPacketCounts()
		dumpSummary.PrintWarnings()

		send()
	}

	if args.TelemetryInterval > 0 {
		ticker := time.NewTicker(time.Duration(args.TelemetryInterval) * time.Second)

		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				req.ObservedDurationInSeconds = int(now.Sub(req.ObservedStartingAt) / time.Second)
				send()
			}
		}
	}
}

type interfaceError struct {
	interfaceName string
	err           error
}

// Captures packets from the network and adds them to a trace. The trace is
// created if it doesn't already exist.
func Run(args Args) error {
	args.lint()

	// During debugging, capture packets not matching the user's filters so we can
	// report statistics on those packets.
	capturingNegation := viper.GetBool("debug")

	if capturingNegation {
		printer.Debugln("Capturing filtered traffic for debugging.")
	}

	// Get the interfaces to listen on.
	interfaces, err := getEligibleInterfaces(args.Interfaces)
	if err != nil {
		return errors.Wrap(err, "failed to list network interfaces")
	}

	// Build the user-specified filter and its negation for each interface.
	userFilters, negationFilters, err := createBPFFilters(interfaces, args.Filter, capturingNegation, 0)
	if err != nil {
		return err
	}
	printer.Debugln("User-specified BPF filters:", userFilters)
	if capturingNegation {
		printer.Debugln("Negation BPF filters:", negationFilters)
	}

	traceTags := collectTraceTags(&args)

	// Build path filters.
	pathExclusions, err := compileRegexps(args.PathExclusions, "path exclusion")
	if err != nil {
		return err
	}
	hostExclusions, err := compileRegexps(args.HostExclusions, "host exclusion")
	if err != nil {
		return err
	}
	pathAllowlist, err := compileRegexps(args.PathAllowlist, "path filter")
	if err != nil {
		return err
	}
	hostAllowlist, err := compileRegexps(args.HostAllowlist, "host filter")
	if err != nil {
		return err
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
		} else {
			if args.LearnSessionLifetime != time.Duration(0) {
				return errors.Errorf("Cannot automatically rotate sessions when a session name is provided.")
			}
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
	filterSummary := trace.NewPacketCounter()
	negationSummary := trace.NewPacketCounter()

	numUserFilters := len(pathExclusions) + len(hostExclusions) + len(pathAllowlist) + len(hostAllowlist)
	prefilterSummary := trace.NewPacketCounter()

	// Initialized shared rate object, if we are configured with a rate limit
	var rateLimit *trace.SharedRateLimit
	if args.WitnessesPerMinute != 0.0 {
		rateLimit = trace.NewRateLimit(args.WitnessesPerMinute)
		defer rateLimit.Stop()
	}

	// Backend collectors that need trace rotation
	var toRotate []trace.LearnSessionCollector

	dumpSummary := NewSummary(
		capturingNegation,
		interfaces,
		negationFilters,
		numUserFilters,
		filterSummary,
		prefilterSummary,
		negationSummary,
	)

	// Synchronization for collectors + collector errors, each of which is run in a separate goroutine.
	var doneWG sync.WaitGroup
	doneWG.Add(len(userFilters) + len(negationFilters))
	errChan := make(chan interfaceError, len(userFilters)+len(negationFilters)) // buffered enough so it never blocks
	stop := make(chan struct{})

	// If we're sending traffic to the cloud, then start telemetry and stop
	// when the main collection process does.
	if args.Out.AkitaURI != nil {
		go SendTelemetry(&args, learnClient, backendSvc, dumpSummary, stop)
	}

	// Start collecting -- set up one or two collectors per interface, depending on whether filters are in use
	numCollectors := 0
	for _, filterState := range []filterState{matchedFilter, notMatchedFilter} {
		var summary *trace.PacketCounter
		var filters map[string]string
		if filterState == matchedFilter {
			filters = userFilters
			summary = filterSummary
		} else {
			filters = negationFilters
			summary = negationSummary
		}

		for interfaceName, filter := range filters {
			var collector trace.Collector

			// Build collectors from the inside out (last applied to first applied).
			//  8. Back-end collector (sink).
			//  7. Statistics.
			//  6. Subsampling.
			//  5. Path and host filters.
			//  4. Eliminate Akita CLI traffic.
			//  3. Count packets before user filters for diagnostics.
			//  2. Process TLS traffic into TLS-connection metadata.
			//  1. Aggregate TCP-packet metadata into TCP-connection metadata.

			// Back-end collector (sink).
			if filterState == notMatchedFilter {
				// During debugging, we capture the negation of the user's filters. This
				// allows us to report statistics for packets not matching the user's
				// filters. We need to avoid sending this traffic to the back end,
				// however.
				collector = trace.NewDummyCollector()
			} else {
				var localCollector trace.Collector
				if args.Out.LocalPath != nil {
					if lc, err := createLocalCollector(interfaceName, *args.Out.LocalPath, traceTags); err == nil {
						localCollector = lc
					} else {
						return err
					}
				}

				var backendCollector trace.Collector
				if args.Out.AkitaURI != nil && args.Out.LocalPath != nil {
					backendCollector = trace.NewBackendCollector(backendSvc, backendLrn, learnClient, args.Plugins)
					collector = trace.TeeCollector{
						Dst1: backendCollector,
						Dst2: localCollector,
					}
				} else if args.Out.AkitaURI != nil {
					backendCollector = trace.NewBackendCollector(backendSvc, backendLrn, learnClient, args.Plugins)
					collector = backendCollector
				} else if args.Out.LocalPath != nil {
					collector = localCollector
				} else {
					return errors.Errorf("invalid output location")
				}

				// If the backend collector supports rotation of learn session ID, then set that up.
				if lsc, ok := backendCollector.(trace.LearnSessionCollector); ok && lsc != nil {
					toRotate = append(toRotate, lsc)
				}
			}

			// Statistics.
			//
			// Count packets that have *passed* filtering (so that we know whether the
			// trace is empty or not.)  In the future we could add columns for both
			// pre- and post-filtering.
			collector = &trace.PacketCountCollector{
				PacketCounts: summary,
				Collector:    collector,
			}

			// Subsampling.
			collector = trace.NewSamplingCollector(args.SampleRate, collector)
			if rateLimit != nil {
				collector = rateLimit.NewCollector(collector)
			}

			// Path and host filters.
			if len(hostExclusions) > 0 {
				collector = trace.NewHTTPHostFilterCollector(hostExclusions, collector)
			}
			if len(pathExclusions) > 0 {
				collector = trace.NewHTTPPathFilterCollector(pathExclusions, collector)
			}
			if len(hostAllowlist) > 0 {
				collector = trace.NewHTTPHostAllowlistCollector(hostAllowlist, collector)
			}
			if len(pathAllowlist) > 0 {
				collector = trace.NewHTTPPathAllowlistCollector(pathAllowlist, collector)
			}

			// Eliminate Akita CLI traffic, unless --dogfood has been specified
			if !viper.GetBool("dogfood") {
				collector = &trace.UserTrafficCollector{
					Collector: collector,
				}
			}

			// Count packets before user filters for diagnostics
			if filterState == matchedFilter && numUserFilters > 0 {
				collector = &trace.PacketCountCollector{
					PacketCounts: prefilterSummary,
					Collector:    collector,
				}
			}

			// Process TLS traffic into TLS-connection metadata.
			collector = tls_conn_tracker.NewCollector(collector)

			// Process TCP-packet metadata into TCP-connection metadata.
			collector = tcp_conn_tracker.NewCollector(collector)

			// Compute the share of the page cache that each collection process may use.
			// (gopacket does not currently permit a unified page cache for packet reassembly.)
			bufferShare := 1.0 / float32(len(negationFilters)+len(userFilters))

			numCollectors++
			go func(interfaceName, filter string) {
				defer doneWG.Done()
				// Collect trace. This blocks until stop is closed or an error occurs.
				if err := pcap.Collect(stop, interfaceName, filter, bufferShare, collector, summary); err != nil {
					errChan <- interfaceError{
						interfaceName: interfaceName,
						err:           errors.Wrapf(err, "failed to collect trace on interface %s", interfaceName),
					}
				}
			}(interfaceName, filter)
		}
	}

	if len(toRotate) > 0 && args.LearnSessionLifetime != time.Duration(0) {
		printer.Debugf("Rotating learn sessions with interval %v\n", args.LearnSessionLifetime)
		go RotateLearnSession(&args, stop, toRotate, learnClient, backendSvc, traceTags)
	}

	{
		iNames := make([]string, 0, len(interfaces))
		for n := range interfaces {
			iNames = append(iNames, n)
		}
		printer.Stderr.Infof("Running learn mode on interfaces %s\n", strings.Join(iNames, ", "))
	}

	unfiltered := true
	for _, f := range userFilters {
		if f != "" {
			unfiltered = false
			break
		}
	}
	if unfiltered {
		printer.Stderr.Warningf("%s\n", printer.Color.Yellow("--filter flag is not set, this means that all network traffic is treated as your API traffic"))
	}

	// Keep track of errors by interface, as well as errors from the subcommand
	// if applicable.
	errorsByInterface := make(map[string]error)
	var subcmdErr error
	if args.ExecCommand != "" {
		printer.Stderr.Infof("Running subcommand...\n\n\n")

		time.Sleep(pcapStartWaitTime)

		// Print delimiter so it's easier to differentiate subcommand output from
		// Akita output.
		// It won't appear in JSON-formatted output.
		printer.Stdout.RawOutput(subcommandOutputDelimiter)
		printer.Stderr.RawOutput(subcommandOutputDelimiter)
		cmdErr := runCommand(args.ExecCommandUser, args.ExecCommand)
		printer.Stdout.RawOutput(subcommandOutputDelimiter)
		printer.Stderr.RawOutput(subcommandOutputDelimiter)

		if cmdErr != nil {
			subcmdErr = errors.Wrap(cmdErr, "failed to run subcommand")
			// We promised to preserve the subcommand's exit code.
			// Explicitly notify whoever is running us to exit.
			if exitErr, ok := errors.Cause(subcmdErr).(*exec.ExitError); ok {
				subcmdErr = util.ExitError{
					ExitCode: exitErr.ExitCode(),
					Err:      subcmdErr,
				}
			}
		} else {
			// Check if we have any errors on our side.
			select {
			case interfaceErr := <-errChan:
				printer.Stderr.Errorf("Encountered errors while collecting traces, stopping...\n")
				errorsByInterface[interfaceErr.interfaceName] = interfaceErr.err

				// Drain errChan.
			DoneClearingChannel:
				for {
					select {
					case interfaceErr := <-errChan:
						errorsByInterface[interfaceErr.interfaceName] = interfaceErr.err
					default:
						break DoneClearingChannel
					}
				}
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
			sig := make(chan os.Signal, 2)
			signal.Notify(sig, os.Interrupt)
			signal.Notify(sig, syscall.SIGTERM)

			// Continue until an interrupt or all collectors have stopped with errors.
		DoneWaitingForSignal:
			for {
				select {
				case received := <-sig:
					printer.Stderr.Infof("Received %v, stopping trace collection...\n", received.String())
					break DoneWaitingForSignal
				case interfaceErr := <-errChan:
					errorsByInterface[interfaceErr.interfaceName] = interfaceErr.err

					if len(errorsByInterface) < numCollectors {
						printer.Stderr.Errorf("Encountered an error on interface %s, continuing with remaining interfaces.  Error: %s\n", interfaceErr.interfaceName, interfaceErr.err.Error())
					} else {
						printer.Stderr.Errorf("Encountered an error on interface %s.  Error: %s\n", interfaceErr.interfaceName, interfaceErr.err.Error())
						break DoneWaitingForSignal
					}
				}
			}
		}
	}

	time.Sleep(pcapStopWaitTime)

	// Signal all processors to stop.
	close(stop)

	// Wait for processors to exit.
	doneWG.Wait()
	printer.Stderr.Infof("Trace collection stopped\n")

	// Print errors per interface.
	if len(errorsByInterface) > 0 {
		printer.Stderr.Errorf("Encountered errors on %d / %d interfaces\n", len(errorsByInterface), numCollectors)
		for interfaceName, err := range errorsByInterface {
			printer.Stderr.Errorf("%12s %s\n", interfaceName, err)
		}

		// If collectors on all interfaces report errors, report trace
		// collection failed.
		if len(errorsByInterface) == numCollectors {
			return errors.Errorf("trace collection failed")
		}
	}

	// If a subcommand was supplied and failed, surface the failure.
	if subcmdErr != nil {
		return errors.Wrap(subcmdErr, "trace collection failed")
	}

	// Print warnings
	dumpSummary.PrintWarnings()

	if dumpSummary.IsEmpty() {
		return errors.New("API trace is empty")
	}

	printer.Stderr.Infof("%s 🎉\n\n", printer.Color.Green("Success!"))
	return nil
}

func createLocalCollector(interfaceName, outDir string, tags map[tags.Key]string) (trace.Collector, error) {
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

	return trace.NewHARCollector(interfaceName, outDir, tags), nil
}
