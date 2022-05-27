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

	// reader is set to the reader side of the output pipe. It does not have mapFuncs
	// applied, they are applied at aggregation time. When the command exits, the error
	// should be raised from the reader after the reader's buffer is exhausted.
	reader io.ReadCloser

	// mapFuncs define LineMaps to be applied at aggregation time.
	mapFuncs []LineMap

	// waitAndCloseFunc should only be called via doWaitOnce(). It should wait for command
	// exit and handle setting an error such that once reads from reader are complete, the
	// reader should return the error from the command.
	waitAndCloseFunc func() error
	waitAndCloseOnce sync.Once
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
	fileBuffersSize := maxBufferSize / int64(4)
	outputBuffer := buffer.NewUnboundedBuffer(maxBufferSize, fileBuffersSize)
	// We need to retain a copy of stderr for error creation.
	stderrCopy := buffer.NewUnboundedBuffer(maxBufferSize, fileBuffersSize)

	// We use this buffered pipe from github.com/djherbis/nio that allows async read and
	// write operations to the reader and writer portions of the pipe respectively.
	outputReader, outputWriter := nio.Pipe(outputBuffer)

	// Set up output hooks
	switch attach {
	case attachCombined:
		cmd.Stdout = outputWriter
		cmd.Stderr = io.MultiWriter(stderrCopy, outputWriter)

	case attachOnlyStdOut:
		cmd.Stdout = outputWriter
		cmd.Stderr = stderrCopy

	case attachOnlyStdErr:
		cmd.Stdout = nil // discard
		cmd.Stderr = io.MultiWriter(stderrCopy, outputWriter)

	default:
		return NewErrorOutput(fmt.Errorf("unexpected attach type %d", attach))
	}

	// Start command execution
	if err := cmd.Start(); err != nil {
		return NewErrorOutput(err)
	}

	return &commandOutput{
		ctx:    ctx,
		reader: outputReader,
		waitAndCloseFunc: func() error {
			err := newError(cmd.Wait(), stderrCopy)
			// CloseWithError makes it so that when all output has been consumed from the
			// reader, the given error is returned.
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
	go o.waitAndClose()

	_, err := o.mappedLinePipe(newLineWriter(dst), nil)
	return err
}

func (o *commandOutput) Lines() ([]string, error) {
	go o.waitAndClose()

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

	result, err := execJQ(o.ctx, jqCode, o)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (o *commandOutput) String() (string, error) {
	b, err := io.ReadAll(o)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}

func (o *commandOutput) Read(p []byte) (int, error) {
	go o.waitAndClose()

	return o.reader.Read(p)
}

// WriteTo implements io.WriterTo, and returns int64 instead of int because of:
// https://stackoverflow.com/questions/29658892/why-does-io-writertos-writeto-method-return-an-int64-rather-than-an-int
func (o *commandOutput) WriteTo(dst io.Writer) (int64, error) {
	go o.waitAndClose()

	if len(o.mapFuncs) == 0 {
		// Happy path, directly pipe output
		return io.Copy(dst, o.reader)
	}

	return o.mappedLinePipe(dst, nil)
}

func (o *commandOutput) Wait() error {
	err := o.waitAndClose()
	// Wait does not consume output, so prevent further reads from occuring.
	o.reader.Close()
	return err
}

// goWaitOnce waits for command completion and closes the write half of the reader. Most
// callers do not need to use the returned error - operations that read from o.reader
// should return the error from that instead, which in most cases should be the same error.
func (o *commandOutput) waitAndClose() error {
	// If err is not reset by waitAndCloseOnce.Do, then output has already been consumed,
	// and we raise this default error.
	err := fmt.Errorf("output has already been consumed")
	o.waitAndCloseOnce.Do(func() {
		err = o.waitAndCloseFunc()
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
