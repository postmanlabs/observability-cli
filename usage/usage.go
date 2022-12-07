package usage

import (
	"os"
	"sync"
	"time"

	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/go-utils/math"
	"github.com/akitasoftware/go-utils/queues"
	"github.com/c9s/goprocinfo/linux"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/printer"
)

const (
	selfStatusFile = "/proc/self/status"
	selfStatFile   = "/proc/self/stat"
	allStatFile    = "/proc/stat"

	// Writing clearRefsCode to clearRefsFile resets the VM high-water mark.
	clearRefsCode = "5"
	clearRefsFile = "/proc/self/clear_refs"
)

type statHistory struct {
	observedAt time.Time
	stat       *linux.ProcessStat
	status     *linux.ProcessStatus
	allStat    *linux.Stat
}

// Compute a 1-hour sliding window.
const (
	slidingWindowSize = time.Hour
)

var (
	agentResourceUsage      *api_schema.AgentResourceUsage
	agentResourceUsageMutex sync.Mutex

	// Contains up to N samples, one per polling interval, where
	// N = slidingWindowSize/pollingInterval.
	history queues.Queue[statHistory]

	// Peak usage over the lifetime of the agent.
	peakCoresUsed   float64
	peakRelativeCPU float64
	peakVM          uint64

	// Only report errors during the first polling operation.
	finishedFirstPoll bool

	// Ensure at most one worker is polling.
	isPolling      bool
	isPollingMutex sync.Mutex
)

// Returns a 1-hour sliding window reflecting this Akita agent's CPU and
// memory usage, updated every pollingInterval minutes, or nil if resource
// usage is unavailable.
//
// The polling interval is set by the first call to Poll().  If the interval
// doesn't evenly divide a 1-hour sliding window, the sliding window becomes
// the largest multiple of the polling interval less than 1 hour, or simply
// the polling interval if longer than 1 hour.
func Get() *api_schema.AgentResourceUsage {
	agentResourceUsageMutex.Lock()
	defer agentResourceUsageMutex.Unlock()

	return agentResourceUsage
}

// Waits delay seconds, then starts polling resource usage every N seconds.
// If another process has already started polling, this call has no effect.
// Use Get() to get the latest usage data.
func Poll(done <-chan struct{}, delay time.Duration, pollingInterval time.Duration) {
	// Check if polling is disabled.
	if pollingInterval <= 0 {
		return
	}

	// Return if isPolling is already true to ensure only one thread is polling.
	if testAndSetPolling(true) {
		return
	}
	defer testAndSetPolling(false)

	history = queues.NewLinkedListQueue[statHistory]()

	// Immediately record /proc state to compare against later.
	stat, status, allStat, err := readProcFS()
	if err != nil {
		printer.Infof("Unable to poll for agent resource usage: %s\n", err)
		return
	}

	history.Enqueue(statHistory{
		observedAt: time.Now(),
		stat:       stat,
		status:     status,
		allStat:    allStat,
	})

	// Record usage after delay, then transition into recording every
	// pollingInterval.
	if delay >= 0 {
		time.Sleep(delay)

		if err := poll(pollingInterval); err != nil {
			printer.Infof("Unable to poll for agent resource usage: %s\n", err)
			return
		}
	}

	// Record usage every pollingInterval.
	ticker := time.NewTicker(pollingInterval)
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := poll(pollingInterval); err != nil {
				printer.Infof("Unable to poll for agent resource usage: %s\n", err)
				return
			}
		}
	}
}

func poll(pollingInterval time.Duration) error {
	log := func(msg string, args ...any) {
		if finishedFirstPoll {
			return
		}
		printer.Infof(msg, args...)
	}

	stat, status, allStat, err := readProcFS()
	if err != nil {
		return err
	}

	oldestStats, exist := history.Peek().Get()
	if !exist {
		// Should never happen.
		return errors.Errorf("Internal error: missing resource usage history.")
	}

	lastStat := oldestStats.stat
	lastAllStat := oldestStats.allStat

	// Compute the processing time for this process vs. all processes since
	// the last time Get() or Init() was called.
	selfCPU := float64(stat.Utime-lastStat.Utime) + float64(stat.Stime-lastStat.Stime)
	allCPU := float64(allStat.CPUStatAll.User-lastAllStat.CPUStatAll.User) +
		float64(allStat.CPUStatAll.System-lastAllStat.CPUStatAll.System) +
		float64(allStat.CPUStatAll.Idle-lastAllStat.CPUStatAll.Idle)

	relativeCPU := selfCPU / allCPU
	peakRelativeCPU = math.Max(peakRelativeCPU, relativeCPU)

	coresUsed := relativeCPU * float64(len(allStat.CPUStats))
	peakCoresUsed = math.Max(peakCoresUsed, coresUsed)

	// Get highest recorded VM high water mark.
	vmHWM := status.VmHWM
	history.ForEach(func(h statHistory) {
		vmHWM = math.Max(vmHWM, h.status.VmHWM)
	})

	peakVM = math.Max(peakVM, vmHWM)

	// Update history.
	observedAt := time.Now()
	history.Enqueue(statHistory{
		observedAt: observedAt,
		stat:       stat,
		status:     status,
		allStat:    allStat,
	})

	// If the history has filled the sliding window, evict the oldest.  There
	// will always be at least one element in the history.
	if history.Size() > math.Max(int(slidingWindowSize/pollingInterval), 1) {
		history.Dequeue()
	}

	// Clear VM high-water mark.
	if f, err := os.Open(clearRefsFile); err != nil {
		log("Failed to clear VmHWM.  Memory usage telemetry will report the high-water mark as computed by /proc/self/status.\n")
	} else {
		defer f.Close()
		if _, err := f.Write([]byte(clearRefsCode)); err != nil {
			log("Failed to clear VmHWM.  Memory usage telemetry will report the high-water mark as computed by /proc/self/status.\n")
		}
	}

	observedDuration := observedAt.Sub(oldestStats.observedAt)
	observedStartingAt := oldestStats.observedAt

	// Update the usage data.
	agentResourceUsageMutex.Lock()
	defer agentResourceUsageMutex.Unlock()

	agentResourceUsage = &api_schema.AgentResourceUsage{
		Recent: api_schema.AgentResourceUsageData{
			CoresUsed:   coresUsed,
			RelativeCPU: relativeCPU,
			VmHWM:       vmHWM,
		},
		Peak: api_schema.AgentResourceUsageData{
			CoresUsed:   peakCoresUsed,
			RelativeCPU: peakRelativeCPU,
			VmHWM:       peakVM,
		},
		ObservedStartingAt:        observedStartingAt,
		ObservedDurationInSeconds: int(observedDuration.Seconds()),
	}

	finishedFirstPoll = true
	return nil
}

func readProcFS() (stat *linux.ProcessStat, status *linux.ProcessStatus, allStat *linux.Stat, err error) {
	status, err = linux.ReadProcessStatus(selfStatusFile)
	if err != nil {
		return nil, nil, nil, errors.Errorf("Failed to load %s.  Resource usage telemetry will not be available.", selfStatusFile)
	}

	stat, err = linux.ReadProcessStat(selfStatFile)
	if err != nil {
		return nil, nil, nil, errors.Errorf("Failed to load %s.  Resource usage telemetry will not be available.", selfStatFile)
	}

	allStat, err = linux.ReadStat(allStatFile)
	if err != nil {
		return nil, nil, nil, errors.Errorf("Failed to load %s.  Resource usage telemetry will not be available.", allStatFile)
	}

	return stat, status, allStat, nil
}

// Sets isPolling and returns its old value.
func testAndSetPolling(v bool) bool {
	isPollingMutex.Lock()
	defer isPollingMutex.Unlock()

	old := isPolling
	isPolling = v

	return old
}
