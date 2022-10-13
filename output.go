package run

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/djherbis/nio/v3"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	StreamLines(dst func(line string)) error
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
//
// All aggregation functions should take care that all output collected is returned,
// regardless of whether read operations return errors.
type commandOutput struct {
	ctx context.Context

	// reader is set to the reader side of the output pipe. It does not have mapFuncs
	// applied, they are applied at aggregation time. When the command exits, the error
	// should be raised from the reader after the reader's buffer is exhausted.
	reader io.ReadCloser

	// mapFuncs define LineMaps to be applied at aggregation time.
	mapFuncs lineMaps

	// mappedData is set by incremental aggregators like Read, and holds mapped results.
	mappedData io.Reader

	// waitAndCloseFunc should only be called via doWaitOnce(). It should wait for command
	// exit and handle setting an error such that once reads from reader are complete, the
	// reader should return the error from the command.
	waitAndCloseFunc func() error
	waitAndCloseOnce sync.Once
}

var _ Output = &commandOutput{}

type attachedOutput int

const (
	attachCombined   attachedOutput = 0
	attachOnlyStdOut attachedOutput = 1
	attachOnlyStdErr attachedOutput = 2
)

// attachOutputAndRun is called by (*Command).Run() to start command execution and collect
// command output.
func attachAndRun(
	ctx context.Context,
	attachOutput attachedOutput,
	attachInput io.Reader,
	executedCmd ExecutedCommand,
) Output {
	// Set up command
	cmd := exec.CommandContext(ctx, executedCmd.Args[0], executedCmd.Args[1:]...)
	cmd.Dir = executedCmd.Dir
	cmd.Env = executedCmd.Environ
	cmd.Stdin = attachInput

	// Prepare tracing
	tracer, attrs := getTracer(ctx)
	// span should manually be ended in error scenarios - make sure each code path that
	// should end the span appropriately ends the span before returning.
	var span trace.Span
	ctx, span = tracer.Start(ctx, "Run "+cmd.Path, trace.WithAttributes(attrs(executedCmd)...))

	// Set up buffers for output and errors - we need to retain a copy of stderr for error
	// creation.
	var outputBuffer, stderrCopy = makeUnboundedBuffer(), makeUnboundedBuffer()

	// We use this buffered pipe from github.com/djherbis/nio that allows async read and
	// write operations to the reader and writer portions of the pipe respectively.
	outputReader, outputWriter := nio.Pipe(outputBuffer)

	// Set up output hooks
	switch attachOutput {
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
		err := fmt.Errorf("unexpected attach type %d", attachOutput)
		span.RecordError(err)
		span.SetStatus(codes.Error, "")
		span.End()
		return NewErrorOutput(err)
	}

	// Log and start command execution
	if log := getLogger(ctx); log != nil {
		log(executedCmd)
	}
	if err := cmd.Start(); err != nil {
		err := fmt.Errorf("failed to start command: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "")
		span.End()
		return NewErrorOutput(err)
	}

	output := &commandOutput{
		ctx:    ctx,
		reader: outputReader,
	}

	output.waitAndCloseFunc = func() error {
		// In the happy case, this is where we end the span - when the command finishes
		// and all resources are closed.
		defer span.End()

		err := newError(cmd.Wait(), stderrCopy)
		span.AddEvent("Done") // add done event because some time may elapse before span end
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "")
		}

		// CloseWithError makes it so that when all output has been consumed from the
		// reader, the given error is returned.
		outputWriter.CloseWithError(err)

		return err
	}

	return output
}

func (o *commandOutput) Map(f LineMap) Output {
	o.mapFuncs = append(o.mapFuncs, f)
	return o
}

func (o *commandOutput) Stream(dst io.Writer) error {
	trace.SpanFromContext(o.ctx).AddEvent("Stream")

	_, err := o.WriteTo(dst)
	return err
}

func (o *commandOutput) StreamLines(dst func(line string)) error {
	trace.SpanFromContext(o.ctx).AddEvent("StreamLines")

	go o.waitAndClose()

	_, err := o.mapFuncs.Pipe(o.ctx, o.reader, newLineWriter(func(b []byte) {
		dst(string(b))
	}), nil)
	return err
}

func (o *commandOutput) Lines() ([]string, error) {
	trace.SpanFromContext(o.ctx).AddEvent("Lines")

	go o.waitAndClose()

	// export lines
	linesC := make(chan string, 3)
	errC := make(chan error)
	go func() {
		dst := newLineWriter(func(line []byte) { linesC <- string(line) })
		_, err := o.mapFuncs.Pipe(o.ctx, o.reader, dst, func() { close(linesC) })
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
	trace.SpanFromContext(o.ctx).AddEvent("JQ")

	jqCode, err := buildJQ(query)
	if err != nil {
		// Record this error because it is not related to reading/writing
		trace.SpanFromContext(o.ctx).RecordError(err)
		return nil, err
	}

	return execJQ(o.ctx, jqCode, o)
}

func (o *commandOutput) String() (string, error) {
	trace.SpanFromContext(o.ctx).AddEvent("String")

	var sb strings.Builder
	if err := o.Stream(&sb); err != nil {
		return sb.String(), err
	}
	return strings.TrimSuffix(sb.String(), "\n"), nil
}

func (o *commandOutput) Read(p []byte) (int, error) {
	trace.SpanFromContext(o.ctx).AddEvent("Read")

	go o.waitAndClose()

	if len(o.mapFuncs) == 0 {
		// Happy path, just read
		return o.reader.Read(p)
	}

	// Otherwise, we can only really read the whole thing and send the data back bit by
	// bit as read requests come in.
	if o.mappedData == nil {
		reader, writer := nio.Pipe(makeUnboundedBuffer())
		go func() {
			_, err := o.mapFuncs.Pipe(o.ctx, o.reader, writer, nil)
			writer.CloseWithError(err)
		}()
		o.mappedData = reader
	}

	return o.mappedData.Read(p)
}

// WriteTo implements io.WriterTo, and returns int64 instead of int because of:
// https://stackoverflow.com/questions/29658892/why-does-io-writertos-writeto-method-return-an-int64-rather-than-an-int
func (o *commandOutput) WriteTo(dst io.Writer) (int64, error) {
	trace.SpanFromContext(o.ctx).AddEvent("WriteTo")

	go o.waitAndClose()

	if len(o.mapFuncs) == 0 {
		// Happy path, directly pipe output
		return io.Copy(dst, o.reader)
	}

	return o.mapFuncs.Pipe(o.ctx, o.reader, dst, nil)
}

func (o *commandOutput) Wait() error {
	trace.SpanFromContext(o.ctx).AddEvent("Wait")

	err := o.waitAndClose()
	// Wait does not consume output, so prevent further reads from occuring.
	o.reader.Close()
	return err
}

// waitAndClose waits for command completion and closes the write half of the reader. Most
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
