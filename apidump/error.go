package apidump

import (
	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/pkg/errors"
)

type ApidumpError struct {
	err error

	// Type of error to be reported as part of error telemetry.
	errType api_schema.ApidumpErrorType
}

func NewApidumpError(errType api_schema.ApidumpErrorType, msg string) ApidumpError {
	return ApidumpError{
		err:     errors.New(msg),
		errType: errType,
	}
}

func NewApidumpErrorf(errType api_schema.ApidumpErrorType, format string, args ...interface{}) ApidumpError {
	return ApidumpError{
		err:     errors.Errorf(format, args...),
		errType: errType,
	}
}

func (e ApidumpError) Error() string {
	return e.err.Error()
}

// Returns the error type if err contains an ApidumpError, or ApidumpError_Other
// otherwise.
func GetErrorType(err error) api_schema.ApidumpErrorType {
	return GetErrorTypeWithDefault(err, api_schema.ApidumpError_Other)
}

// Returns the error type if err contains an ApidumpError, or def otherwise.
func GetErrorTypeWithDefault(err error, def api_schema.ApidumpErrorType) api_schema.ApidumpErrorType {
	var apidumpErr ApidumpError
	if ok := errors.As(err, &apidumpErr); ok {
		return apidumpErr.errType
	}
	return def
}
