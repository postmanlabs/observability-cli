package util

import (
	"fmt"
)

type ExitError struct {
	ExitCode int
	Err      error
}

func (ee ExitError) Error() string {
	return fmt.Sprintf("exit with code %d: %v", ee.ExitCode, ee.Err)
}
