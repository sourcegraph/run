package run

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

// runError wraps exec.ExitError such that it always includes the embedded stderr.
type runError struct{ execErr *exec.ExitError }

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
			// Not assigned by default using cmd.Start(), so we consume our copy of stderr
			// and set it here.
			exitErr.Stderr = bytes.TrimSpace(stdErr.Bytes())
			stdErr.Reset()
		}
		return &runError{execErr: exitErr}
	}

	return err
}

func (e *runError) Error() string {
	if len(e.execErr.Stderr) == 0 {
		return e.execErr.String()
	}
	return fmt.Sprintf("%s: %s", e.execErr.String(), string(e.execErr.Stderr))
}

// ExitCode returns the exit code if set, or 0 otherwise (including if the error is nil).
//
// Implements https://sourcegraph.com/github.com/urfave/cli/-/blob/errors.go?L79&subtree=true
func (e *runError) ExitCode() int {
	return e.execErr.ExitCode()
}
