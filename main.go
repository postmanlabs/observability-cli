package main

import (
	"github.com/akitasoftware/akita-cli/cmd"
	"github.com/akitasoftware/akita-cli/usage"
)

func main() {
	// Initialize state for computing CPU usage.  Fails if /proc files are not
	// available, in which case usage won't be included in telemetry stats.
	_ = usage.Init()

	cmd.Execute()
}
