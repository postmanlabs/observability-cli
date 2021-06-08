package trace

import (
	"math/rand"
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

type requestKey struct {
	StreamID       string
	SequenceNumber int
}

type rateLimitCollector struct {
	// Next collector in stack
	NextCollector Collector

	// Witnesses per minute (configured value) and per epoch (derived value)
	WitnessesPerMinute float64
	WitnessesPerEpoch  int

	// Current estimate of time taken to capture WitnessesPerEpoch
	EstimatedSampleInterval time.Duration
	FirstEstimate           bool

	// Current epoch: start time, sampling start time, count of witnesses captured
	CurrentEpochStart    time.Time
	SampleIntervalStart  time.Time
	SampleIntervalActive bool
	SampleIntervalCount  int

	// Time for epoch and interval
	EpochTicker   *time.Ticker
	IntervalTimer *time.Timer

	// Channel for incoming captures
	Incoming chan akinet.ParsedNetworkTraffic

	// Map of unmatched request arrival times
	RequestArrivalTimes map[requestKey]time.Time

	// Channel for signaling goroutine to exit
	Done chan struct{}

	// Channle for signaling that the main loop is running
	Running chan struct{}
}

func (r *rateLimitCollector) Process(pnt akinet.ParsedNetworkTraffic) error {
	r.Incoming <- pnt
	return nil
}

func (r *rateLimitCollector) Close() error {
	// TODO: do we need to wait for the worker goroutine to exit?
	close(r.Done)
	r.NextCollector.Close()
	return nil
}

func (r *rateLimitCollector) Run() {
	// Start the first epoch
	r.EpochTicker = time.NewTicker(viper.GetDuration(RateLimitEpochTime))
	defer r.EpochTicker.Stop()

	r.CurrentEpochStart = time.Now()
	r.startInterval(time.Now())

	// Set up the timer so it's non-nil, but immediately stop it
	r.IntervalTimer = time.NewTimer(0)
	stopTimer := func() {
		if !r.IntervalTimer.Stop() {
			<-r.IntervalTimer.C
		}
	}
	stopTimer()
	defer stopTimer()

	// Main loop: handle events as they come in, exit when Done
	for true {
		select {
		case <-r.Done:
			close(r.Running)
			return
		case epochStart := <-r.EpochTicker.C:
			stopTimer()
			r.startNewEpoch(epochStart)
		case intervalStart := <-r.IntervalTimer.C:
			r.startInterval(intervalStart)
		case r.Running <- struct{}{}:
			break
		case pnt := <-r.Incoming:
			r.handlePacket(pnt)
		}
	}
}

func (r *rateLimitCollector) startNewEpoch(epochStart time.Time) {
	r.CurrentEpochStart = time.Now()
	printer.Debugln("New collection epoch:", r.CurrentEpochStart)

	// Pick a time for the next sampling interval to start within this epoch.
	if r.FirstEstimate {
		// Didn't get a new estimate, just keep collecting everything.
		r.IntervalTimer.Reset(0)
	} else {
		upperBound := viper.GetDuration(RateLimitEpochTime) - r.EstimatedSampleInterval
		randomOffset := time.Duration(rand.Int63n(int64(upperBound)))
		r.IntervalTimer.Reset(randomOffset)
	}

	// Check for old requests in map
	threshold := epochStart.Add(-1 * viper.GetDuration(RateLimitMaxDuration))
	expired := 0
	for k, v := range r.RequestArrivalTimes {
		if v.Before(threshold) {
			delete(r.RequestArrivalTimes, k)
			expired += 1
		}
	}
	printer.Debugf("Expired %v old requests\n", expired)
}

func (r *rateLimitCollector) startInterval(start time.Time) {
	// If we're in the current interval, just reset and keeping going.
	// We don't get an updated interval that way, but that's OK.
	printer.Debugln("New sample interval started:", start)
	r.SampleIntervalStart = start
	r.SampleIntervalCount = 0
	r.SampleIntervalActive = true
}

func (r *rateLimitCollector) endInterval(end time.Time) {
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

func (r *rateLimitCollector) handlePacket(pnt akinet.ParsedNetworkTraffic) {
	switch c := pnt.Content.(type) {
	case akinet.HTTPRequest:
		if r.SampleIntervalActive {
			// Collect request and the matching response as well.
			r.NextCollector.Process(pnt)
			key := requestKey{c.StreamID.String(), c.Seq}
			r.RequestArrivalTimes[key] = pnt.ObservationTime
			// Bump count and see if we've hit our budget
			r.SampleIntervalCount += 1
			if r.SampleIntervalCount >= r.WitnessesPerEpoch {
				r.endInterval(pnt.FinalPacketTime)
			}
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
		// All non-HTTP requests are passed through so they can
		// be counted, if we're in an interval, but don't (yet) count
		// against the witness budget.
		// (For example, we might want to start counting source/dest pairs
		// for HTTPS, or otherwise recording unparsable network traffic.)
		if r.SampleIntervalActive {
			r.NextCollector.Process(pnt)
		}
	}
}

func NewRateLimiter(witnessesPerMinute float64, collector Collector) Collector {
	witnessLimit := witnessesPerMinute * viper.GetDuration(RateLimitEpochTime).Minutes()
	if witnessLimit < 1 {
		printer.Warningln("Witnesses per minute rate is too low, rounding up to 1.")
		witnessLimit = 1
	}
	c := &rateLimitCollector{
		NextCollector:       collector,
		WitnessesPerMinute:  witnessesPerMinute,
		WitnessesPerEpoch:   int(witnessLimit),
		FirstEstimate:       true,
		Incoming:            make(chan akinet.ParsedNetworkTraffic, viper.GetInt(RateLimitQueueDepth)),
		RequestArrivalTimes: make(map[requestKey]time.Time),
		Done:                make(chan struct{}),
		Running:             make(chan struct{}),
	}
	go c.Run()
	return c
}
