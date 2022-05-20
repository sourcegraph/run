package run

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
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
	ctx context.Context

	// reader is set to one of stdErr, stdOut, or both. It does not have mapFuncs
	// applied, they are applied at aggregation time.
	reader io.Reader

	// mapFuncs define LineMaps to be applied at aggregation time.
	mapFuncs []LineMap

	// waitFunc is called before aggregation exit.
	waitFunc func() (error, io.Reader)

	// finalized is true if aggregator has already been consumed.
	finalized bool
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
	closers := []io.Closer{outputReader, outputWriter}

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
		waitFunc: func() (error, io.Reader) {
			cmdErr := cmd.Wait()

			// Stop all read/write operations
			for _, closer := range closers {
				closer.Close()
			}

			return cmdErr, stderrCopy
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
	mapsErrC := make(chan error)
	go func() {
		_, err := o.mappedLinePipe(newLineWriter(dst), nil)
		mapsErrC <- err
	}()

	// Wait for command to finish
	err := o.Wait()

	// Wait for aggregation to finish
	mapErr := <-mapsErrC

	if err != nil {
		return err
	}
	return mapErr
}

func (o *commandOutput) Lines() ([]string, error) {
	// export lines
	linesC := make(chan string, 3)
	sendLine := func(line []byte) { linesC <- string(line) }
	closeLines := func() { close(linesC) }

	// start collecting lines
	mapsErrC := make(chan error)
	go func() {
		dst := newLineWriter(sendLine)
		_, err := o.mappedLinePipe(dst, closeLines)
		mapsErrC <- err
	}()

	// aggregate lines from results
	aggregatedC := make(chan []string)
	go func() {
		lines := make([]string, 0, 10)
		for line := range linesC {
			lines = append(lines, line)
		}
		aggregatedC <- lines
	}()

	// wait for command to finish
	err := o.Wait()

	// Wait for results
	results := <-aggregatedC

	// done
	if err != nil {
		return results, err
	}
	return results, <-mapsErrC
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

func (o *commandOutput) Read(read []byte) (int, error) {
	if o.finalized {
		return 0, io.EOF
	}

	// Stream output to a buffer
	buffer := bytes.NewBuffer(make([]byte, 0, len(read)))
	err := o.Stream(buffer)
	defer buffer.Reset()

	// Populate data
	for i, b := range buffer.Bytes() {
		if i < len(read) {
			read[i] = b
		}
	}

	return buffer.Len() + 1, err
}

// WriteTo implements io.WriterTo, and returns int64 instead of int because of:
// https://stackoverflow.com/questions/29658892/why-does-io-writertos-writeto-method-return-an-int64-rather-than-an-int
func (o *commandOutput) WriteTo(dst io.Writer) (int64, error) {
	if len(o.mapFuncs) == 0 {
		// Happy path, directly pipe output
		doneC := make(chan int64)
		go func() {
			written, _ := io.Copy(dst, o.reader)
			doneC <- written
		}()
		errC := make(chan error)
		go func() {
			errC <- o.Wait()
		}()
		return <-doneC, <-errC
	}

	// Pipe output via the maped line pipe
	mapErrC := make(chan error)
	writtenC := make(chan int64)
	go func() {
		written, err := o.mappedLinePipe(dst, nil)
		mapErrC <- err // send err first because we receive this first later
		writtenC <- written
	}()

	// Wait for command to finish
	err := o.Wait()

	// Wait for results
	mapErr := <-mapErrC

	if err != nil {
		return <-writtenC, err
	}
	return <-writtenC, mapErr
}

func (o *commandOutput) Wait() error {
	if o.finalized {
		return errors.New("output aggregator has already been finalized")
	}
	o.finalized = true
	return newError(o.waitFunc())
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

	return totalWritten, nil
}
