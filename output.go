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
	// Map adds a LineMap function to be applied to this Output. It is only applied at
	// aggregation time using e.g. Stream, Lines, and so on. Multiple LineMaps are applied
	// sequentially, with the result of previous LineMaps propagated to subsequent
	// LineMaps.
	Map(f LineMap) Output

	// TODO wishlist functionality
	// Mode(mode OutputMode) Output

	// Stream writes mapped output from the command to the destination writer until
	// command completion.
	Stream(dst io.Writer) error
	// StreamLines writes mapped output from the command and sends it line by line to the
	// destination callback until command completion.
	StreamLines(dst func(line []byte)) error
	// Lines waits for command completion and aggregates mapped output from the command.
	Lines() ([]string, error)
	// JQ waits for command completion executes a JQ query against the entire output.
	//
	// Refer to https://github.com/itchyny/gojq for the specifics of supported syntax.
	JQ(query string) ([]byte, error)
	// Reader is implemented so that Output can be provided directly to another Command
	// using Input().
	io.Reader
	// WriterTo is implemented for convenience when chaining commands in LineMap.
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

var maxBufferSize int64 = 128 * 1024

func attachOutputAndRun(ctx context.Context, cmd *exec.Cmd) Output {
	closers := make([]io.Closer, 0, 3*2)

	// Use unbounded buffers that create files of size fileBuffersSize to store overflow.
	//
	// TODO: We might be able to use ring buffers here to recycle memory, but it doesn't
	// seem to work with large inputs out of the box - data can end up truncated before
	// reads complete.
	fileBuffersSize := maxBufferSize / int64(4)
	combinedBuffer := buffer.NewUnboundedBuffer(maxBufferSize, fileBuffersSize)
	stdoutBuffer := buffer.NewUnboundedBuffer(maxBufferSize, fileBuffersSize)
	stderrBuffer := buffer.NewUnboundedBuffer(maxBufferSize, fileBuffersSize)

	// Pipe for combined output
	combinedReader, combinedWriter := nio.Pipe(combinedBuffer)
	closers = append(closers, combinedReader, combinedWriter)

	// Pipe stdout
	stdoutReader, stdoutWriter := nio.Pipe(stdoutBuffer)
	closers = append(closers, stdoutReader, stdoutWriter)
	cmd.Stdout = io.MultiWriter(stdoutWriter, combinedWriter)

	// Pipe stderr. We use a custom pipe because cmd.StderrPipe seems to have side effects
	// that breaks io.MultiWriter, which we need to retain a copy of stderr for error
	// creation.
	stderrReader, stderrWriter := nio.Pipe(stderrBuffer)
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

				// Stop all read/write operations
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

func (o *commandOutput) Map(f LineMap) Output {
	o.aggregator.mapFuncs = append(o.aggregator.mapFuncs, f)
	return o
}
