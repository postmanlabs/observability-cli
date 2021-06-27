package trace

import (
	"math/rand"
	"sync"
	"time"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/akinet"
	"github.com/spf13/viper"
)

const (
	// One sample is collected per epoch
	RateLimitEpochTime = "rate-limit-epoch-time"

	// Maximum time to remember a request which we selected, but haven't seen a reaponse.
	RateLimitMaxDuration = "rate-limit-max-duration"

	// Channel size for packets coming in to collector
	RateLimitQueueDepth = "rate-limit-queue-depth"

	// Parameter controlling exponential moving average
	RateLimitExponentialAlpha = "rate-limit-exponential-alpha"
)

func init() {
	viper.SetDefault(RateLimitEpochTime, 5*time.Minute)
	viper.SetDefault(RateLimitMaxDuration, 10*time.Minute)
	viper.SetDefault(RateLimitQueueDepth, 1000)
	viper.SetDefault(RateLimitExponentialAlpha, 0.3)
}

type SharedRateLimit struct {
	// Current epoch: start time, sampling start time, count of witnesses captured
	CurrentEpochStart    time.Time
	SampleIntervalStart  time.Time
	SampleIntervalActive bool
	SampleIntervalCount  int

	// Time for epoch and interval
	epochTicker   *time.Ticker
	intervalTimer *time.Timer

	// Witnesses per minute (configured value) and per epoch (derived value)
	WitnessesPerMinute float64
	WitnessesPerEpoch  int

	// Current estimate of time taken to capture WitnessesPerEpoch
	EstimatedSampleInterval time.Duration
	FirstEstimate           bool

	// Channel for signaling goroutine to exit
	done chan struct{}

	// Child collectors
	children []*rateLimitCollector

	lock sync.Mutex
}

func (r *SharedRateLimit) startInterval(start time.Time) {
	// If we're in the current interval, just reset and keeping going.
	// We don't get an updated interval that way, but that's OK.
	printer.Debugln("New sample interval started:", start)

	r.lock.Lock()
	defer r.lock.Unlock()
	r.SampleIntervalStart = start
	r.SampleIntervalCount = 0
	r.SampleIntervalActive = true
}

func (r *SharedRateLimit) IntervalStarted() bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.SampleIntervalActive
}

// End the current interval and update the estimate; should be called with
// r.Lock already held.
func (r *SharedRateLimit) endInterval(end time.Time) {
	printer.Debugln("End of sample interval:", end)
	intervalLength := end.Sub(r.SampleIntervalStart)
	r.SampleIntervalActive = false
	r.SampleIntervalCount = 0

	if r.FirstEstimate {
		r.EstimatedSampleInterval = intervalLength
		r.FirstEstimate = false
	} else {
		alpha := viper.GetFloat64(RateLimitExponentialAlpha)
		exponentialMovingAverage :=
			(1-alpha)*float64(r.EstimatedSampleInterval) + alpha*float64(intervalLength)
		printer.Debugln("New estimate:", exponentialMovingAverage)
		r.EstimatedSampleInterval = time.Duration(uint64(exponentialMovingAverage))
	}
}

// Run should immediately starts a measurement interval for the first epoch;
// without an estimate we should start capturing right away on the assumption we'll stay
// below the limit.
func (r *SharedRateLimit) run() {
	r.epochTicker = time.NewTicker(viper.GetDuration(RateLimitEpochTime))
	defer r.epochTicker.Stop()

	r.intervalTimer = time.NewTimer(0)
	defer r.intervalTimer.Stop()

	// Main loop: handle time events or shutdown
	for true {
		select {
		case <-r.done:
			return
		case epochStart := <-r.epochTicker.C:
			r.startNewEpoch(epochStart)
		case intervalStart := <-r.intervalTimer.C:
			r.startInterval(intervalStart)
		}
	}
}

func (r *SharedRateLimit) Stop() {
	close(r.done)
}

func (r *SharedRateLimit) startNewEpoch(epochStart time.Time) {
	printer.Debugln("New collection epoch:", epochStart)

	r.lock.Lock()
	defer r.lock.Unlock()

	r.CurrentEpochStart = time.Now()

	// Ensure timer is stopped before reset
	// I *think* we don't need to drain the channel here (which would require
	// extra state to do reliably anyway.)
	r.intervalTimer.Stop()

	// Pick a time for the next sampling interval to start within this epoch.
	if r.FirstEstimate {
		// Didn't get a new estimate, just keep collecting everything.
		r.intervalTimer.Reset(0)
	} else {
		upperBound := viper.GetDuration(RateLimitEpochTime) - r.EstimatedSampleInterval
		randomOffset := time.Duration(rand.Int63n(int64(upperBound)))
		r.intervalTimer.Reset(randomOffset)
	}

	// Trigger request expiration for all collectors.
	// If they never have Process() called, then their channel will fill up,
	// so do this in a nonblocking fashion (with buffer size 1).
	threshold := epochStart.Add(-1 * viper.GetDuration(RateLimitMaxDuration))
	for _, child := range r.children {
		select {
		case child.epochCh <- threshold:
			continue
		default:
			printer.Debugf("child collector %p not accepting epoch start\n", child)
		}
	}
}

// Check if request should be sampled; increase the count by one.
func (r *SharedRateLimit) AllowHTTPRequest() bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	if !r.SampleIntervalActive {
		return false
	}

	r.SampleIntervalCount += 1
	if r.SampleIntervalCount >= r.WitnessesPerEpoch {
		r.endInterval(time.Now())
	}
	return true
}

// Check if a non-HTTP packet should be sampled.
// All non-HTTP requests are passed through so they can
// be counted, if we're in an interval, but don't (yet) count
// against the witness budget.
// (For example, we might want to start counting source/dest pairs
// for HTTPS, or otherwise recording unparsable network traffic.)func (r *SharedRateLimit) AllowOther() bool {
func (r *SharedRateLimit) AllowOther() bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.SampleIntervalActive
}

type requestKey struct {
	StreamID       string
	SequenceNumber int
}

type rateLimitCollector struct {
	// Shared rate limit across all collectors
	// (typically one is created per interface, and for outgoing vs. incoming)
	RateLimit *SharedRateLimit

	// Next collector in stack
	NextCollector Collector

	// Map of unmatched request arrival times
	RequestArrivalTimes map[requestKey]time.Time

	// Channel from RateLimit for epoch starts
	epochCh chan time.Time
}

func (r *SharedRateLimit) NewCollector(next Collector) Collector {
	c := &rateLimitCollector{
		RateLimit:           r,
		NextCollector:       next,
		RequestArrivalTimes: make(map[requestKey]time.Time),
		epochCh:             make(chan time.Time, 1),
	}
	r.lock.Lock()
	defer r.lock.Unlock()
	r.children = append(r.children, c)
	return c
}

func (r *SharedRateLimit) onCollectorClose(closed *rateLimitCollector) {
	r.lock.Lock()
	defer r.lock.Unlock()
	for i, c := range r.children {
		if c == closed {
			r.children = append(r.children[:i], r.children[i+1:]...)
			return
		}
	}
}

func (r *rateLimitCollector) Process(pnt akinet.ParsedNetworkTraffic) error {
	switch c := pnt.Content.(type) {
	case akinet.HTTPRequest:
		if r.RateLimit.AllowHTTPRequest() {
			// Collect request and the matching response as well.
			r.NextCollector.Process(pnt)
			key := requestKey{c.StreamID.String(), c.Seq}
			r.RequestArrivalTimes[key] = pnt.ObservationTime
		}
	case akinet.HTTPResponse:
		// Collect iff the request is in our map. (This means responses to calls
		// before the sampling interval will not be captured.)
		key := requestKey{c.StreamID.String(), c.Seq}
		if _, ok := r.RequestArrivalTimes[key]; ok {
			delete(r.RequestArrivalTimes, key)
			r.NextCollector.Process(pnt)
		}
	default:
		if r.RateLimit.AllowOther() {
			r.NextCollector.Process(pnt)
		}
	}

	// Check for new epoch, in a nonblocking way
	select {
	case epochStart := <-r.epochCh:
		r.expireRequests(epochStart)
	default:
		break
	}

	return nil
}

func (r *rateLimitCollector) Close() error {
	// Remove self from future epoch updates
	r.RateLimit.onCollectorClose(r)
	return r.NextCollector.Close()
}

// expire requests that came in before the threshold value
func (r *rateLimitCollector) expireRequests(threshold time.Time) {
	expired := 0
	for k, v := range r.RequestArrivalTimes {
		if v.Before(threshold) {
			delete(r.RequestArrivalTimes, k)
			expired += 1
		}
	}
	printer.Debugf("Expired %v old requests\n", expired)
}

func NewRateLimit(witnessesPerMinute float64) *SharedRateLimit {
	witnessLimit := witnessesPerMinute * viper.GetDuration(RateLimitEpochTime).Minutes()
	if witnessLimit < 1 {
		printer.Warningln("Witnesses per minute rate is too low; rounding up to 1 per 5 minutes.")
		witnessLimit = 1
	}
	r := &SharedRateLimit{
		WitnessesPerMinute: witnessesPerMinute,
		WitnessesPerEpoch:  int(witnessLimit),
		FirstEstimate:      true,
		done:               make(chan struct{}),
	}

	// Start the first epoch.
	// run() will start an interval immediately.
	r.CurrentEpochStart = time.Now()
	go r.run()
	return r
}
