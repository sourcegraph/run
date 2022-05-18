package run

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
)

// aggregator implements output finalization and formatting, and should be embedded in
// commandOutput to fulfill the Output interface.
type aggregator struct {
	ctx context.Context

	// reader is set to one of stdErr, stdOut, or both. It does not have mapFuncs
	// applied, they are applied at aggregation time.
	reader io.Reader

	// mapFuncs define LineMaps to be applied at aggregation time.
	mapFuncs []LineMap

	// waitFunc is called before aggregation exit.
	waitFunc func() (error, *bytes.Buffer)

	// finalized is true if aggregator has already been consumed.
	finalized bool
}

func (a *aggregator) Stream(dst io.Writer) error {
	_, err := a.WriteTo(dst)
	return err
}

func (a *aggregator) StreamLines(dst func(line []byte)) error {
	mapsErrC := make(chan error)
	go func() {
		_, err := a.mappedLinePipe(newLineWriter(dst), nil)
		mapsErrC <- err
	}()

	// Wait for command to finish
	err := a.Wait()

	// Wait for aggregation to finish
	mapErr := <-mapsErrC

	if err != nil {
		return err
	}
	return mapErr
}

func (a *aggregator) Lines() ([]string, error) {
	// export lines
	linesC := make(chan string, 3)
	sendLine := func(line []byte) { linesC <- string(line) }
	closeLines := func() { close(linesC) }

	// start collecting lines
	mapsErrC := make(chan error)
	go func() {
		dst := newLineWriter(sendLine)
		_, err := a.mappedLinePipe(dst, closeLines)
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
	err := a.Wait()

	// Wait for results
	results := <-aggregatedC

	// done
	if err != nil {
		return results, err
	}
	return results, <-mapsErrC
}

func (a *aggregator) JQ(query string) ([]byte, error) {
	jqCode, err := buildJQ(query)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	if err := a.Stream(&buffer); err != nil {
		return nil, err
	}

	b, err := execJQ(a.ctx, jqCode, buffer.Bytes())
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (a *aggregator) Read(read []byte) (int, error) {
	if a.finalized {
		return 0, io.EOF
	}

	// Stream output to a buffer
	buffer := bytes.NewBuffer(make([]byte, 0, len(read)))
	err := a.Stream(buffer)
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
func (a *aggregator) WriteTo(dst io.Writer) (int64, error) {
	if len(a.mapFuncs) == 0 {
		// Happy path, directly pipe output
		doneC := make(chan int64)
		go func() {
			written, _ := io.Copy(dst, a.reader)
			doneC <- written
		}()
		errC := make(chan error)
		go func() {
			errC <- a.Wait()
		}()
		return <-doneC, <-errC
	}

	// Pipe output via the maped line pipe
	mapErrC := make(chan error)
	writtenC := make(chan int64)
	go func() {
		written, err := a.mappedLinePipe(dst, nil)
		mapErrC <- err // send err first because we receive this first later
		writtenC <- written
	}()

	// Wait for command to finish
	err := a.Wait()

	// Wait for results
	mapErr := <-mapErrC

	if err != nil {
		return <-writtenC, err
	}
	return <-writtenC, mapErr
}

func (a *aggregator) Wait() error {
	if a.finalized {
		return errors.New("output aggregator has already been finalized")
	}
	a.finalized = true
	return newError(a.waitFunc())
}

func (a *aggregator) mappedLinePipe(dst io.Writer, close func()) (int64, error) {
	if close != nil {
		defer close()
	}

	scanner := bufio.NewScanner(a.reader)

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

		for _, f := range a.mapFuncs {
			tb := &tracedBuffer{Buffer: &buf}
			buffered, err := f(a.ctx, line, tb)
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
