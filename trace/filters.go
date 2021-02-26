package trace

import (
	"regexp"

	"github.com/akitasoftware/akita-cli/learn"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/akitasoftware/akita-libs/trackers"
)

// Filters out HTTP paths.
func NewHTTPPathFilterCollector(matchers []*regexp.Regexp, col Collector) Collector {
	return &genericRequestFilter{
		Collector: col,
		filterFunc: func(r akinet.HTTPRequest) bool {
			if r.URL != nil {
				for _, m := range matchers {
					if m.MatchString(r.URL.Path) {
						return false
					}
				}
			}
			return true
		},
	}
}

func NewHTTPHostFilterCollector(matchers []*regexp.Regexp, col Collector) Collector {
	return &genericRequestFilter{
		Collector: col,
		filterFunc: func(r akinet.HTTPRequest) bool {
			for _, m := range matchers {
				if m.MatchString(r.Host) {
					return false
				}
			}
			return true
		},
	}
}

// Filters out third-party trackers.
func New3PTrackerFilterCollector(col Collector) Collector {
	return &genericRequestFilter{
		Collector: col,
		filterFunc: func(r akinet.HTTPRequest) bool {
			if r.URL != nil {
				return !trackers.IsTrackerDomain(r.URL.Host)
			}
			return true
		},
	}
}

// Generic filter collector to filter out requests that match a custom filter
// function. Handles filtering out the corresponding responses as well.
type genericRequestFilter struct {
	Collector Collector

	// Returns true if the request should be included.
	filterFunc func(akinet.HTTPRequest) bool

	// Records witness IDs of filtered requests so we can filter out the
	// corresponding responses.
	// NOTE: we're assuming that we always see the request before the
	// corresponding response, which should be generally true, with the exception
	// of observing a response without request due to packet capture starting
	// mid-connection.
	filteredIDs map[akid.WitnessID]struct{}
}

func (fc *genericRequestFilter) Process(t akinet.ParsedNetworkTraffic) error {
	include := true
	switch c := t.Content.(type) {
	case akinet.HTTPRequest:
		if fc.filterFunc != nil && !fc.filterFunc(c) {
			include = false

			if fc.filteredIDs == nil {
				fc.filteredIDs = map[akid.WitnessID]struct{}{}
			}
			fc.filteredIDs[learn.ToWitnessID(c.StreamID, c.Seq)] = struct{}{}
		}
	case akinet.HTTPResponse:
		if _, ok := fc.filteredIDs[learn.ToWitnessID(c.StreamID, c.Seq)]; ok {
			include = false
		}
	}

	if include {
		return fc.Collector.Process(t)
	}
	return nil
}

func (fc *genericRequestFilter) Close() error {
	return fc.Collector.Close()
}
