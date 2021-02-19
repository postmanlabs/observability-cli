package trace

import (
	"github.com/akitasoftware/akita-libs/akinet"
)

// Not to be confused with coffee collector.
type TeeCollector struct {
	Dst1 Collector
	Dst2 Collector
}

func (tc TeeCollector) Process(t akinet.ParsedNetworkTraffic) error {
	err1 := tc.Dst1.Process(t)
	err2 := tc.Dst2.Process(t)

	if err1 != nil {
		return err1
	}
	return err2
}

func (tc TeeCollector) Close() error {
	err1 := tc.Dst1.Close()
	err2 := tc.Dst2.Close()

	if err1 != nil {
		return err1
	}
	return err2
}
