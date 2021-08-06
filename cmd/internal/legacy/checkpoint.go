package legacy

import (
	"time"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-libs/akid"
)

// Creates a checkpoint and prints out progress message while we wait.
func checkpointWithProgress(c rest.LearnClient, lrn akid.LearnSessionID, timeout time.Duration) (akid.APISpecID, string, error) {
	type specPair struct {
		SpecID akid.APISpecID
		SpecName string
	}
	checkpointResult := make(chan specPair)
	checkpointErr := make(chan error)
	go func() {
		specID, specName, err := checkpointLearnSession(c, lrn, timeout)
		if err != nil {
			checkpointErr <- err
		} else {
			checkpointResult <- specPair{specID, specName}
		}
	}()

	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case r := <-checkpointResult:
			return r.SpecID, r.SpecName, nil
		case err := <-checkpointErr:
			return akid.APISpecID{}, "", err
		case <-t.C:
			printer.Stderr.Infoln("Still working...")
		}
	}
}
