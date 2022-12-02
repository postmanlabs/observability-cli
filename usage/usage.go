package usage

import (
	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/c9s/goprocinfo/linux"
	"github.com/pkg/errors"
)

const (
	selfStatusFile = "/proc/self/status"
	selfStatFile   = "/proc/self/stat"
	allStatFile    = "/proc/stat"
)

var (
	lastStat    *linux.ProcessStat
	lastAllStat *linux.Stat
)

// Initialize state for computing CPU/memory usage for telemetry.
func Init() {
	// No need to check errors here. If /proc files aren't available,
	// subsequent calls to Get() will return nil.
	lastAllStat, _ = linux.ReadStat(allStatFile)
	lastStat, _ = linux.ReadProcessStat(selfStatFile)
}

// Computes the CPU usage of this process relative to all processes scheduled
// since Get() or Init() was last called.  Also returns the peak virtual memory
// usage of the Akita agent.
//
// Returns nil, nil if proc files are not available.  Fails if Init() has not
// been called.
func Get() (*api_schema.AgentUsage, error) {
	status, err := linux.ReadProcessStatus(selfStatusFile)
	if err != nil {
		return nil, nil
	}

	stat, err := linux.ReadProcessStat(selfStatFile)
	if err != nil {
		return nil, nil
	}

	allStat, err := linux.ReadStat(allStatFile)
	if err != nil {
		return nil, nil
	}

	if lastAllStat == nil || lastStat == nil {
		// If we get this far, it means proc files are available but Init()
		// wasn't called.
		return nil, errors.Errorf("called usage.Get() without usage.Init()")
	}

	// Compute the processing time for this process vs. all processes since
	// the last time Get() or Init() was called.
	selfCPU := float64(stat.Utime-lastStat.Utime) + float64(stat.Stime-lastStat.Stime)
	allCPU := float64(allStat.CPUStatAll.User-lastAllStat.CPUStatAll.User) +
		float64(allStat.CPUStatAll.System-lastAllStat.CPUStatAll.System) +
		float64(allStat.CPUStatAll.Idle-lastAllStat.CPUStatAll.Idle)

	return &api_schema.AgentUsage{
		RelativeCPU: selfCPU / allCPU,
		VMPeak:      status.VmPeak,
	}, nil
}
