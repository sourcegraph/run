package run

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/djherbis/buffer"
	"github.com/djherbis/nio/v3"
)

// Output configures output and aggregation from a command.
//
// It is behind an interface to more easily enable mock outputs and build different types
// of outputs, such as multi-outputs and error-only outputs, without complicating the core
// commandOutput implementation.
type Output interface {
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
	// Lines waits for command completion and aggregates mapped output from the command as
	// a slice of lines.
	Lines() ([]string, error)
	// Lines waits for command completion and aggregates mapped output from the command as
	// a combined string.
	String() (string, error)
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
	ctx context.Context

	// reader is set to one of stdErr, stdOut, or both. It does not have mapFuncs
	// applied, they are applied at aggregation time.
	reader io.ReadCloser

	// mapFuncs define LineMaps to be applied at aggregation time.
	mapFuncs []LineMap

	// waitFunc is called before aggregation exit. It should only be called via doWaitOnce().
	waitFunc func() error
	waitOnce sync.Once
}

var _ Output = &commandOutput{}

var maxBufferSize int64 = 128 * 1024

type attachedOuput int

const (
	attachCombined   = 0
	attachOnlyStdOut = 1
	attachOnlyStdErr = 2
)

func attachOutputAndRun(ctx context.Context, attach attachedOuput, cmd *exec.Cmd) Output {
	// Use unbounded buffers that create files of size fileBuffersSize to store overflow.
	//
	// TODO: We might be able to use ring buffers here to recycle memory, but it doesn't
	// seem to work with large inputs out of the box - data can end up truncated before
	// reads complete.
	fileBuffersSize := maxBufferSize / int64(4)
	outputBuffer := buffer.NewUnboundedBuffer(maxBufferSize, fileBuffersSize)
	outputReader, outputWriter := nio.Pipe(outputBuffer)

	// Set up output hooks
	switch attach {
	case attachCombined:
		cmd.Stdout = outputWriter
		cmd.Stderr = outputWriter

	case attachOnlyStdOut:
		cmd.Stdout = outputWriter

	case attachOnlyStdErr:
		cmd.Stderr = outputWriter

	default:
		return NewErrorOutput(fmt.Errorf("unexpected attach type %d", attach))
	}

	// We need to retain a copy of stderr for error creation.
	stderrCopy := buffer.NewUnboundedBuffer(maxBufferSize, fileBuffersSize)
	if cmd.Stderr != nil {
		cmd.Stderr = io.MultiWriter(stderrCopy, cmd.Stderr)
	} else {
		cmd.Stderr = stderrCopy
	}

	// Start command execution
	if err := cmd.Start(); err != nil {
		return NewErrorOutput(err)
	}

	return &commandOutput{
		ctx: ctx,

		// Default to all output
		reader: outputReader,

		// Define cleanup for command
		waitFunc: func() error {
			err := newError(cmd.Wait(), stderrCopy)
			outputWriter.CloseWithError(err)
			return err
		},
	}
}

func (o *commandOutput) Map(f LineMap) Output {
	o.mapFuncs = append(o.mapFuncs, f)
	return o
}

func (o *commandOutput) Stream(dst io.Writer) error {
	_, err := o.WriteTo(dst)
	return err
}

func (o *commandOutput) StreamLines(dst func(line []byte)) error {
	go o.doWaitOnce()

	_, err := o.mappedLinePipe(newLineWriter(dst), nil)
	return err
}

func (o *commandOutput) Lines() ([]string, error) {
	go o.doWaitOnce()

	// export lines
	linesC := make(chan string, 3)
	errC := make(chan error)
	go func() {
		dst := newLineWriter(func(line []byte) { linesC <- string(line) })
		_, err := o.mappedLinePipe(dst, func() { close(linesC) })
		errC <- err
	}()

	// aggregate lines from results
	lines := make([]string, 0, 10)
	for line := range linesC {
		lines = append(lines, line)
	}

	return lines, <-errC
}

func (o *commandOutput) JQ(query string) ([]byte, error) {
	jqCode, err := buildJQ(query)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	if err := o.Stream(&buffer); err != nil {
		return nil, err
	}

	b, err := execJQ(o.ctx, jqCode, buffer.Bytes())
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (o *commandOutput) String() (string, error) {
	var sb strings.Builder
	err := o.Stream(&sb)
	return strings.TrimSuffix(sb.String(), "\n"), err
}

func (o *commandOutput) Read(p []byte) (int, error) {
	go o.doWaitOnce()

	return o.reader.Read(p)
}

// WriteTo implements io.WriterTo, and returns int64 instead of int because of:
// https://stackoverflow.com/questions/29658892/why-does-io-writertos-writeto-method-return-an-int64-rather-than-an-int
func (o *commandOutput) WriteTo(dst io.Writer) (int64, error) {
	go o.doWaitOnce()

	if len(o.mapFuncs) == 0 {
		// Happy path, directly pipe output
		return io.Copy(dst, o.reader)
	}

	return o.mappedLinePipe(dst, nil)
}

func (o *commandOutput) Wait() error {
	err := o.doWaitOnce()
	o.reader.Close()
	return err
}

// goWaitOnce waits for command completion. Most callers do not need to use the returned
// error - o.reader will be closed with the error, so operations that read from o.reader
// can just use the error returned from reads instead.
func (o *commandOutput) doWaitOnce() error {
	// if err is not reset by waitOnce.Do, then output has already been consumed
	err := fmt.Errorf("output has already been consumed")
	o.waitOnce.Do(func() {
		err = o.waitFunc()
	})
	return err
}

func (o *commandOutput) mappedLinePipe(dst io.Writer, close func()) (int64, error) {
	if close != nil {
		defer close()
	}

	scanner := bufio.NewScanner(o.reader)

	// TODO should we introduce API for configuring max capacity?
	// Errors will happen with lengths > 65536
	// const maxCapacity int = longLineLen
	// buf := make([]byte, maxCapacity)
	// scanner.Buffer(buf, maxCapacity)

	var buf bytes.Buffer
	var totalWritten int64
	for scanner.Scan() {
		line := scanner.Bytes()

		// Defaults to true because if no map funcs unset this, then we will write the
		// entire line.
		writeCalled := true

		for _, f := range o.mapFuncs {
			tb := &tracedBuffer{Buffer: &buf}
			buffered, err := f(o.ctx, line, tb)
			if err != nil {
				return totalWritten, err
			}
			writeCalled = tb.writeCalled

			// Nothing written => end
			if buffered == 0 {
				break
			}

			// Copy bytes and reset for the next map
			line = make([]byte, buf.Len())
			copy(line, buf.Bytes())
			buf.Reset()
		}

		// If anything was written, or a write was called even with an ending, treat it as
		// a line and add a line ending for convenience, unless it already has a line
		// ending.
		if writeCalled && !bytes.HasSuffix(line, []byte("\n")) {
			written, err := dst.Write(append(line, '\n'))
			totalWritten += int64(written)
			if err != nil {
				return totalWritten, err
			}
		}

		// Reset for next line
		buf.Reset()
	}

	return totalWritten, scanner.Err()
}
