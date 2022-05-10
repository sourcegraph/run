package run

// ExitCoder is an error that also denotes an exit code to exit with. Users of Output can
// check if an error implements this interface to get the underlying exit code of a
// command execution.
type ExitCoder interface {
	error
	ExitCode() int
}

// ExitCode returns the exit code associated with err if there is one, otherwise 1. If err
// is nil, returns 0.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}

	if exitCoder, ok := err.(ExitCoder); ok {
		return exitCoder.ExitCode()
	}

	return 1
}
