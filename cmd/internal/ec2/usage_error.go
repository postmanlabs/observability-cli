package ec2 

import "fmt"

type UsageError struct {
	err error
}

func (ue UsageError) Error() string {
	return ue.err.Error()
}

func NewUsageError(err error) error {
	return UsageError{err}
}

func UsageErrorf(f string, args ...interface{}) error {
	return NewUsageError(fmt.Errorf(f, args...))
}
