package apidump

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/google/gopacket/pcap"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/architecture"
	"github.com/akitasoftware/akita-cli/env"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
)

// An interface that's compatible with net.Interface so we can use mock
// interfaces in tests.
type interfaceInfo interface {
	Addrs() ([]net.Addr, error)
}

type interfaceWrapper struct {
	addrs []net.Addr
}

func (w interfaceWrapper) Addrs() ([]net.Addr, error) {
	return w.addrs, nil
}

// Show a warning about failure to check permission, and
// return an appropriate top-level error message.
func showPermissionErrors(sampleError error) error {
	// TODO: pcapPermError needs an Unwrap method to use error.Is().  But
	// I don't know what error this is anyway! So we're back to substring search.
	if strings.Contains(sampleError.Error(), "Operation not permitted") {
		// Permission denied == not enough capabilities
		// Are we running as root?
		if os.Geteuid() == 0 {
			if env.InDocker() {
				printer.Warningf("Although you are running as root, this container lacks the CAP_NET_RAW capability.\n")
				printer.Warningf("It might be that you are in a PaaS that disallows packet capture, or the local configuration has disabled that privilege by default.\n")
				return NewApidumpError(api_schema.ApidumpError_PCAPPermission, "Insufficient permissions in container.")
			} else {
				printer.Warningf("Although you are running as root, the Akita agent lacks the CAP_NET_RAW capability.\n")
				printer.Warningf("It might be that you are in a restricted environment which disallows packet capture, even as the root user.\n")
				return NewApidumpError(api_schema.ApidumpError_PCAPPermission, "Insufficient permissions.")
			}
		} else {
			// Non-root user
			printer.Warningf("The agent needs the CAP_NET_RAW capability to capture packets. You are running as an unprivileged (non-root) user.\n")
			return NewApidumpError(api_schema.ApidumpError_PCAPPermission, "Insufficient permissions, try using \"sudo\" to run as root.")
		}
	} else if strings.Contains(sampleError.Error(), "SIOCETHTOOL(ETHTOOL_GET_TS_INFO) ioctl failed: Function not implemented") {
		// This happens when the binary was built for a different architecture, e.g.
		// if the user pulled the amd64 Docker image on arm64.
		arch := architecture.GetCanonicalArch()

		printer.Warningf(
			"The agent received \"Function not implemented\" when trying to read from your network interfaces. "+
				"This often indicates that the Akita agent was built for a different architecture than your host architecture. "+
				"This Akita agent binary was built for %s.\n",
			arch,
		)

		if env.InDocker() {
			return NewApidumpErrorf(
				api_schema.ApidumpError_PCAPInterfaceNotImplemented,
				"Unable to read network interfaces. If your host architecture is not %s, try using "+
					"`docker pull --platform $YOUR_ARCHITECTURE docker.postman.com/postman-lc-agent:latest` to pull an Akita agent "+
					"built for your architecture.",
				arch,
			)
		} else {
			return NewApidumpErrorf(
				api_schema.ApidumpError_PCAPInterfaceNotImplemented,
				"Unable to read network interfaces. If your host architecture is not %s, try using the Akita install script: `bash -c \"$(curl -L https://releases.akita.software/scripts/install_akita.sh)\"`",
				arch,
			)
		}
	}

	// Some other failure cause.
	// TODO: Known errors without error-specific help:
	//   * "The device is not up"
	printer.Warningf("The agent could not access any network interfaces. Please contact\n")
	printer.Warningf("observability-support@postman.com with the log messages above.\n")
	return NewApidumpError(api_schema.ApidumpError_PCAPInterfaceOther, "Error while checking permissions.")
}

// Get the list of interface names that we should listen on. By default, this is
// all interfaces on the machine that are up. User may override this with
// --interface flag.
func getEligibleInterfaces(userSpecified []string) (map[string]interfaceInfo, error) {
	if len(userSpecified) > 0 {
		results := make(map[string]interfaceInfo, len(userSpecified))
		for _, n := range userSpecified {
			iface, err := net.InterfaceByName(n)
			if err != nil {
				return nil, errors.Wrapf(err, "interface %s not found", n)
			}
			results[n] = iface
		}

		ifaceErrs := checkPcapPermissions(results)
		for i, err := range ifaceErrs {
			// Return error if we're not able to listen on a user-specified interface.
			printer.Errorf("Error on interface %q: %v\n", i, err)
			return nil, showPermissionErrors(err)
		}
		return results, nil
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, errors.Wrap(err, "--interface is not set and failed to get interfaces automatically")
	}
	results := make(map[string]interfaceInfo, len(ifaces))
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp != 0 {
			// Extract the addresses now instead of taking a pointer to iface and
			// storing it in results because the pointee changes.
			addrs, err := iface.Addrs()
			if err != nil {
				return nil, errors.Wrapf(err, "failed to get addresses for interface %s", iface.Name)
			}
			if len(addrs) == 0 {
				printer.Warningf("Skipping interface %s because it has no addresses\n", iface.Name)
				continue
			}
			results[iface.Name] = interfaceWrapper{addrs: addrs}
		}
	}

	// Don't return error if we're unable to listen to one of the available
	// interfaces, and just listen to the interfaces we have the permissions
	// for.
	ifaceErrs := checkPcapPermissions(results)
	var sampleError error
	for ifaceName, err := range ifaceErrs {
		printer.Warningf("Skipping interface %s for collecting packets because of error: %v\n", ifaceName, err)
		sampleError = err
		delete(results, ifaceName)
	}

	if len(results) == 0 {
		// Tailor the message to the error received and the current context.
		if sampleError != nil {
			return nil, showPermissionErrors(sampleError)
		}

		// These two failure would be very odd, because how did we get past looking up a project ID without one?
		if len(ifaces) == 0 {
			if env.InDocker() {
				printer.Warningf("The agent did not see any network interfaces in its container.\n")
				printer.Warningf("Try running Docker with the \"--network host\" flag, or attaching to a container that has a network interface.\n")
			} else {
				printer.Warningf("The agent did not see any network interfaces attached to this machine.\n")
			}
		} else {
			printer.Warningf("The agent could not automatically identify any network interfaces to use\n")
			printer.Warningf("All the interfaces were deselected because they lacked IP addresses. Use the --interfaces flag to manually select one or more network interfaces to use.\n")
		}

		return nil, NewApidumpError(api_schema.ApidumpError_PCAPInterfaceOther, "Failed to automatically find interfaces.")
	}

	return results, nil
}

type pcapPermErr struct {
	iface string
	err   error
}

func (pe pcapPermErr) Error() string {
	return fmt.Sprintf("failed to read packets from interface %s: %v", pe.iface, pe.err)
}

// Check if we have permission to capture packets on the given set of
// interfaces.
func checkPcapPermissions(interfaces map[string]interfaceInfo) map[string]error {
	printer.Debugf("Checking pcap permissions...\n")
	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(len(interfaces))
	errChan := make(chan *pcapPermErr, len(interfaces)) // buffered enough to never block
	for iface := range interfaces {
		go func(iface string) {
			defer wg.Done()
			h, err := pcap.OpenLive(iface, 1600, true, pcap.BlockForever)
			if err != nil {
				telemetry.Error("pcap permissions", err)
				errChan <- &pcapPermErr{iface: iface, err: err}
				return
			}
			h.Close()
		}(iface)
	}

	wg.Wait()
	printer.Debugf("Check pcap permission done after %s\n", time.Since(start))
	close(errChan)
	errs := map[string]error{}
	for pe := range errChan {
		if pe != nil {
			errs[pe.iface] = pe
		}
	}
	return errs
}

// Returns BPF filter for inbound API spec traffic on each interface.
func getInboundBPFFilter(interfaces map[string]interfaceInfo, bpfFilter string, port uint16) (map[string]string, error) {
	results := make(map[string]string, len(interfaces))

	// Respect --bpf-filter flag first, if set.
	if bpfFilter != "" {
		if port > 0 {
			return nil, errors.Errorf("May not specify both --bpf-filter and --port flags.")
		}
		for name := range interfaces {
			results[name] = bpfFilter
		}
		return results, nil
	}

	if port == 0 {
		// No filter, free for all!
		for name := range interfaces {
			results[name] = ""
		}
		return results, nil
	}

	// Build filters based on port and host IPs.
	for name, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get interface addresses")
		}
		ips := make([]net.IP, 0, len(addrs))
		for _, addr := range addrs {
			switch ta := addr.(type) {
			case *net.IPAddr:
				ips = append(ips, ta.IP)
			case *net.IPNet:
				// Only take the IP assigned to the interface, not the full network.
				ips = append(ips, ta.IP)
			case *net.TCPAddr:
				ips = append(ips, ta.IP)
			case *net.UDPAddr:
				ips = append(ips, ta.IP)
			}
		}

		printer.Debugf("Interface %s IPs: %v\n", name, ips)

		// We currently only support TCP/IP stack, so we don't explore the option of
		// filtering by MAC address.
		if len(ips) == 0 {
			return nil, errors.Errorf("cannot find IP addresses on interface %s", name)
		}

		// We assume the CLI is running on the same host as the server, so we
		// require that the port must only match if the packet is destined to the
		// port on this host or if the packet originates from the port on this host.
		// This helps us reduce false positives when the user uses a common
		// port (e.g. 80) since we can treat packets going to third-parties that
		// listen on the same common port as outbound rather than inbound.
		filters := make([]string, 0, len(ips))
		p := strconv.FormatUint(uint64(port), 10)
		for _, ip := range ips {
			for _, dir := range []string{"src", "dst"} {
				f := fmt.Sprintf("(%s host %s and %s port %s)", dir, ip.String(), dir, p)
				filters = append(filters, f)
			}
		}
		results[name] = strings.Join(filters, " or ")
	}
	return results, nil
}

func createBPFFilters(interfaces map[string]interfaceInfo, bpfFilter string, createOutbound bool, port uint16) (map[string]string, map[string]string, error) {
	inboundFilters, err := getInboundBPFFilter(interfaces, bpfFilter, port)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to build BPF filters for inbound traffic")
	}
	// Outbound filters are the negation of inbound filters. This doesn't account
	// for random traffic on the machine, but is the best we can do without an
	// explicit outbound filter flag.
	outboundFilters := make(map[string]string, len(inboundFilters))
	if createOutbound {
		for n, f := range inboundFilters {
			// No inbound filter means that we can't differentiate between inbound and
			// outbound (i.e. user didn't set --port or --bpf-filter).
			if f != "" {
				outboundFilters[n] = fmt.Sprintf("not (%s)", f)
			}
		}
	}

	return inboundFilters, outboundFilters, nil
}
