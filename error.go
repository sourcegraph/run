package run

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

type runError struct {
	nonExecErr error
	execErr    *exec.ExitError
}

var _ error = &runError{}

// newError creats a new *Error, and can be provided a nil error. If stdErrBuffer is not
// nil, consumes and resets it.
func newError(err error, stdErr *bytes.Buffer) error {
	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stdErr != nil {
			// Not assigned by default
			exitErr.Stderr = bytes.TrimSpace(stdErr.Bytes())
		}
		return &runError{
			execErr: exitErr,
		}
	}

	return &runError{
		nonExecErr: err,
	}
}

func (e *runError) Error() string {
	if e.nonExecErr != nil {
		return e.nonExecErr.Error()
	}

	if len(e.execErr.Stderr) == 0 {
		return e.execErr.String()
	}
	return fmt.Sprintf("%s: %s", e.execErr.String(), string(e.execErr.Stderr))
}

// ExitCode returns the exit code if set, or 0 otherwise (including if the error is nil).
//
// Implements https://sourcegraph.com/github.com/urfave/cli/-/blob/errors.go?L79&subtree=true
func (e *runError) ExitCode() int {
	if e.nonExecErr != nil {
		return 1
	}

	return e.execErr.ExitCode()
}
