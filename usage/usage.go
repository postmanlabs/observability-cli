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
	allStatAtProcessStart *linux.Stat
)

// Records CPU processing time statistics at the start of the Akita agent.
// Fails if proc files are not present.
func Init() error {
	var err error

	allStatAtProcessStart, err = linux.ReadStat(allStatFile)
	if err != nil {
		return errors.Wrapf(err, "usage init: failed to load %s", allStatFile)
	}

	return nil
}

// Computes the CPU usage of this process relative to all processes scheduled
// since the Akita agent started.  Also returns the peak virtual memory usage
// of the Akita agent.
//
// Fails if proc files are not present or Init() has not yet been called.
func Get() (*api_schema.AgentUsage, error) {
	status, err := linux.ReadProcessStatus(selfStatusFile)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load %s", selfStatusFile)
	}

	stat, err := linux.ReadProcessStat("/proc/self/stat")
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load %s", selfStatFile)
	}

	allStat, err := linux.ReadStat(allStatFile)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load %s", allStatFile)
	}

	if allStatAtProcessStart == nil {
		// If we get this far, it means proc files are available but Init()
		// wasn't called.
		return nil, errors.Wrap(err, "called GetUsage() without Init()")
	}

	// Compute the processing time for this process vs. all processes since
	// the last time GetUsage or Init was called.
	selfCPU := float64(stat.Utime) + float64(stat.Stime)
	allCPUSinceProcessStart := float64(allStat.CPUStatAll.User-allStatAtProcessStart.CPUStatAll.User) +
		float64(allStat.CPUStatAll.System-allStatAtProcessStart.CPUStatAll.System)

	return &api_schema.AgentUsage{
		RelativeCPU: selfCPU / allCPUSinceProcessStart,
		VMPeak:      status.VmPeak,
	}, nil
}
