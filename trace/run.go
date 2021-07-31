package trace

import (
	"github.com/pkg/errors"

	col "github.com/akitasoftware/akita-cli/pcap"
	"github.com/akitasoftware/akita-libs/akinet"
	akihttp "github.com/akitasoftware/akita-libs/akinet/http"
)

func Collect(stop <-chan struct{}, intf, bpfFilter string, bufferShare float32, proc Collector, packetCount PacketCountConsumer) error {
	defer proc.Close()

	facts := []akinet.TCPParserFactory{
		akihttp.NewHTTPRequestParserFactory(),
		akihttp.NewHTTPResponseParserFactory(),
	}

	parser := col.NewNetworkTrafficParser(bufferShare)

	if packetCount != nil {
		parser.InstallObserver(CountTcpPackets(intf, packetCount))
	}

	parsedChan, err := parser.ParseFromInterface(intf, bpfFilter, stop, facts...)
	if err != nil {
		return errors.Wrap(err, "couldn't start parsing from interface")
	}

	for t := range parsedChan {
		t.Interface = intf
		if err := proc.Process(t); err != nil {
			return err
		}
	}

	return nil
}
