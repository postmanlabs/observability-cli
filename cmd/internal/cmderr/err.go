package cmderr

// Wrapper for all Akita generated error vs CLI parsing error.
// Used to determine whether to print usage message on error.
type AkitaErr struct {
	Err error
}

func (a AkitaErr) Error() string {
	return a.Err.Error()
}

// github.com/pkg/errors causer interface
func (a AkitaErr) Cause() error {
	return a.Err
}

// github.com/pkg/errors Unwrap interface
func (a AkitaErr) Unwrap() error {
	return a.Err
}
