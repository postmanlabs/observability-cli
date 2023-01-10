package apispec

import (
	"io"
	"time"

	"github.com/pkg/errors"
)

func WriteSpec(out io.Writer, spec string) error {
	for len(spec) > 0 {
		if n, err := io.WriteString(out, spec); err != nil {
			return errors.Wrap(err, "failed to write output")
		} else {
			spec = spec[n:]
		}
	}
	return nil
}

// Wait for the reported number of witnesses in each learn session to match the number uploaded.
// Poll at 5 seconds, 15 seconds, and 30 seconds and give up after three tries.
const numWaitAttempts = 3

var waitDelay [numWaitAttempts]time.Duration = [numWaitAttempts]time.Duration{
	5 * time.Second,
	10 * time.Second,
	15 * time.Second,
}
