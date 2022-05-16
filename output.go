package run

import (
	"bytes"
	"context"
	"io"
	"os/exec"

	"github.com/djherbis/buffer"
	"github.com/djherbis/nio/v3"
)

// Output configures output and aggregation from a command.
//
// It is behind an interface to more easily enable mock outputs and build different types
// of outputs, such as multi-outputs and error-only outputs, without complicating the core
// commandOutput implementation.
type Output interface {
	// StdOut configures this Output to only provide StdErr. By default, Output works with
	// combined output.
	StdOut() Output
	// StdErr configures this Output to only provide StdErr. By default, Output works with
	// combined output.
	StdErr() Output
	// Filter adds a filter to this Output. It is only applied at aggregation time using
	// e.g. Stream, Lines, and so on.
	Filter(filter LineFilter) Output

	// TODO wishlist functionality
	// Mode(mode OutputMode) Output

	// Stream writes filtered output from the command to the destination writer until
	// command completion.
	Stream(dst io.Writer) error
	// StreamLines writes filtered output from the command and sends it line by line to the
	// destination callback until command completion.
	StreamLines(dst func(line []byte)) error
	// Lines waits for command completion and aggregates filtered output from the command.
	Lines() ([]string, error)
	// JQ waits for command completion executes a JQ query against the entire output.
	//
	// Refer to https://github.com/itchyny/gojq for the specifics of supported syntax.
	JQ(query string) ([]byte, error)
	// Reader is implemented so that Output can be provided directly to another Command
	// using Input().
	io.Reader
	// WriterTo is implemented for convenience when chaining commands in LineFilter.
	io.WriterTo

	// Wait waits for command completion and returns.
	Wait() error
}

// commandOutput is the core Output implementation, designed to be attached to an exec.Cmd.
//
// It only handles piping output and configuration - aggregation is handled by the embedded
// aggregator.
type commandOutput struct {
	stdOut io.Reader
	stdErr io.Reader

	*aggregator
}

var _ Output = &commandOutput{}

const maxBufferSize = 128 * 1024

func attachOutputAndRun(ctx context.Context, cmd *exec.Cmd) Output {
	closers := make([]io.Closer, 0, 3*2)

	combinedReader, combinedWriter := nio.Pipe(buffer.New(maxBufferSize))
	closers = append(closers, combinedReader, combinedWriter)

	// Pipe stdout
	stdoutReader, stdoutWriter := nio.Pipe(buffer.New(maxBufferSize))
	closers = append(closers, stdoutReader, stdoutWriter)
	cmd.Stdout = io.MultiWriter(stdoutWriter, combinedWriter)

	// Pipe stderr. We use a custom pipe because cmd.StderrPipe seems to have side effects
	// that breaks io.MultiError, which we need to retain a copy of stderr for error
	// creation.
	stderrReader, stderrWriter := nio.Pipe(buffer.New(maxBufferSize))
	closers = append(closers, stderrReader, stderrWriter)
	var stderrCopy bytes.Buffer
	cmd.Stderr = io.MultiWriter(&stderrCopy, stderrWriter, combinedWriter)

	// Start command execution
	if err := cmd.Start(); err != nil {
		return NewErrorOutput(err)
	}

	return &commandOutput{
		stdOut: stdoutReader,
		stdErr: stderrReader,

		aggregator: &aggregator{
			ctx: ctx,

			// Default to all output
			reader: combinedReader,

			// Define cleanup for command
			waitFunc: func() (error, *bytes.Buffer) {
				cmdErr := cmd.Wait()
				for _, closer := range closers {
					closer.Close()
				}
				return cmdErr, &stderrCopy
			},
		},
	}
}

func (o *commandOutput) StdOut() Output {
	o.aggregator.reader = o.stdOut
	return o
}

func (o *commandOutput) StdErr() Output {
	o.aggregator.reader = o.stdErr
	return o
}

func (o *commandOutput) Filter(filter LineFilter) Output {
	o.aggregator.filterFuncs = append(o.aggregator.filterFuncs, filter)
	return o
}
