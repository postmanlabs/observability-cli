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

	clearRefsFile = "/proc/self/clear_refs"
	clearRefsCode = "5"
)

type statHistory struct {
	stat    *linux.ProcessStat
	status  *linux.ProcessStatus
	allStat *linux.Stat
}

// Compute a 1-hour sliding window.
const (
	slidingWindowSize = time.Hour
)

var (
	agentResourceUsage      *api_schema.AgentResourceUsage
	agentResourceUsageMutex sync.Mutex

	// Contains up to N samples, one per polling interval, where
	// N * pollingInterval = slidingWindowSize.
	history queues.Queue[statHistory]

	// Only report errors during the first polling operation.
	finishedFirstPoll bool

	// Ensure at most one worker is polling.
	isPolling      bool
	isPollingMutex sync.Mutex
)

// Returns a 1-hour sliding window reflecting this Akita agent's CPU and
// memory usage, updated every 5 minutes, or nil if resource usage is
// unavailable.
func Get() *api_schema.AgentResourceUsage {
	agentResourceUsageMutex.Lock()
	defer agentResourceUsageMutex.Unlock()

	return agentResourceUsage
}

// Waits delay seconds, then starts polling resource usage every N seconds.  Use Get() to get the latest
// usage data.
func Poll(done <-chan struct{}, delay time.Duration, pollingInterval time.Duration) {
	// Check if polling is disabled.
	if pollingInterval <= 0 {
		return
	}

	// Ensure only one thread is polling.
	if !shouldStartPolling() {
		return
	}
	setIsPolling(true)
	defer setIsPolling(false)

	history = queues.NewLinkedListQueue[statHistory]()

	// Immediately record /proc state to compare against later.
	stat, status, allStat, err := readProcFS()
	if err != nil {
		printer.Infof("Unable to poll for agent resource usage: %s\n", err)
		return
	}

	history.Enqueue(statHistory{
		stat:    stat,
		status:  status,
		allStat: allStat,
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

	observedAt := time.Now()
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
	coresUsed := relativeCPU * float64(len(allStat.CPUStats))

	// Get highest recorded VM high water mark.
	vmHWM := status.VmHWM
	history.ForEach(func(h statHistory) {
		vmHWM = math.Max(vmHWM, h.status.VmHWM)
	})

	// Update history.
	if history.Size() >= int(slidingWindowSize/pollingInterval) {
		history.Dequeue()
		history.Enqueue(statHistory{
			stat:    stat,
			status:  status,
			allStat: allStat,
		})
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

	observedDuration := time.Duration(history.Size()) * pollingInterval
	observedStartingAt := observedAt.Add(-observedDuration)

	// Update the usage data.
	agentResourceUsageMutex.Lock()
	defer agentResourceUsageMutex.Unlock()

	agentResourceUsage = &api_schema.AgentResourceUsage{
		CoresUsed:                 coresUsed,
		RelativeCPU:               relativeCPU,
		VmHWM:                     vmHWM,
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

// Returns true if polling has not already started.
func shouldStartPolling() bool {
	isPollingMutex.Lock()
	defer isPollingMutex.Unlock()

	return !isPolling
}

// Sets the isPolling variable, guarded by isPollingMutex.
func setIsPolling(v bool) {
	isPollingMutex.Lock()
	defer isPollingMutex.Unlock()

	isPolling = v
}
