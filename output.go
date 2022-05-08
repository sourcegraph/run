package run

import (
	"bytes"
	"io"
	"os"
	"os/exec"
)

// LineFilter allows modifications of individual lines from Output.
//
// An explicit "skip" return parameter is required because many bytes library functions
// return nil to denote empty lines, which should be preserved: https://github.com/golang/go/issues/46415
type LineFilter func(line []byte) (newLine []byte, skip bool)

// Output configures output and aggregation from a command.
type Output struct {
	stdOut io.Reader
	stdErr io.Reader

	*aggregator
}

func attachOutputAndRun(cmd *exec.Cmd) *Output {
	closers := make([]io.Closer, 0, 3*2)

	combinedReader, combinedWriter, err := os.Pipe()
	if err != nil {
		return failedToStartOutput(err)
	}
	closers = append(closers, combinedReader, combinedWriter)

	// Pipe stdout
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return failedToStartOutput(err)
	}
	closers = append(closers, stdoutReader, stdoutWriter)
	cmd.Stdout = io.MultiWriter(stdoutWriter, combinedWriter)

	// Pipe stderr. We use a custom pipe because cmd.StderrPipe seems to have side effects
	// that breaks io.MultiError, which we need to retain a copy of stderr for error
	// creation.
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		return failedToStartOutput(err)
	}
	closers = append(closers, stderrReader, stderrWriter)
	var stderrCopy bytes.Buffer
	cmd.Stderr = io.MultiWriter(&stderrCopy, stderrWriter, combinedWriter)

	// Start command execution
	if err := cmd.Start(); err != nil {
		return failedToStartOutput(err)
	}

	return &Output{
		stdOut: stdoutReader,
		stdErr: stderrReader,

		aggregator: &aggregator{
			// Default to all output
			reader: combinedReader,

			// Define cleanup for command
			wait: func() (error, *bytes.Buffer) {
				cmdErr := cmd.Wait()
				for _, closer := range closers {
					closer.Close()
				}
				return cmdErr, &stderrCopy
			},
		},
	}
}

func failedToStartOutput(err error) *Output {
	return &Output{aggregator: &aggregator{parentErr: err}}
}

func (o *Output) StdErr() *Output {
	o.aggregator.reader = o.stdErr
	return o
}

func (o *Output) StdOut() *Output {
	o.aggregator.reader = o.stdOut
	return o
}

func (o *Output) Filter(filter LineFilter) *Output {
	o.aggregator.filterFuncs = append(o.aggregator.filterFuncs, filter)
	return o
}

// TODO
// func (o *Output) Mode(mode OutputMode) *Output {
// 	return o
// }
