package trace

import "github.com/akitasoftware/akita-libs/akinet"

type dummyCollector struct{}

var _ Collector = (*dummyCollector)(nil)

func (*dummyCollector) Process(akinet.ParsedNetworkTraffic) error {
	return nil
}

func (*dummyCollector) Close() error {
	return nil
}

func NewDummyCollector() Collector {
	return &dummyCollector{}
}
