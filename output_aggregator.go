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

	// reader is set to one of stdErr, stdOut, or both. It does not have filterFuncs
	// applied, they are applied at aggregation time.
	reader io.Reader

	// filterFuncs define line filters to be applied at aggregation time.
	filterFuncs []LineFilter

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
	filtersErrC := make(chan error, 1)
	go func() {
		_, err := a.filteredLinePipe(newLineWriter(dst), nil)
		filtersErrC <- err
	}()

	// Wait for command to finish
	err := a.Wait()

	// Wait for aggregation to finish
	filterErr := <-filtersErrC

	if err != nil {
		return err
	}
	return filterErr
}

func (a *aggregator) Lines() ([]string, error) {
	// export lines
	linesC := make(chan string, 3)
	sendLine := func(line []byte) { linesC <- string(line) }
	closeResults := func() { close(linesC) }

	// start collecting lines
	filtersErrC := make(chan error, 1)
	go func() {
		dst := newLineWriter(sendLine)
		_, err := a.filteredLinePipe(dst, closeResults)
		filtersErrC <- err
	}()

	// aggregate lines from results
	resultsC := make(chan []string, 1)
	defer close(resultsC)
	go func() {
		lines := make([]string, 0, 10)
		for line := range linesC {
			lines = append(lines, line)
		}
		resultsC <- lines
	}()

	// wait for command to finish
	err := a.Wait()

	// Wait for results
	results := <-resultsC

	// done
	if err != nil {
		return results, err
	}
	return results, <-filtersErrC
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
	if len(a.filterFuncs) == 0 {
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

	// Pipe output via the filtered line pipe
	filterErrC := make(chan error, 1)
	writtenC := make(chan int64, 1)
	go func() {
		written, err := a.filteredLinePipe(dst, nil)
		writtenC <- written
		filterErrC <- err
	}()

	// Wait for command to finish
	err := a.Wait()

	// Wait for results
	filterErr := <-filterErrC

	if err != nil {
		return <-writtenC, err
	}
	return <-writtenC, filterErr
}

func (a *aggregator) Wait() error {
	if a.finalized {
		return errors.New("output aggregator has already been finalized")
	}
	a.finalized = true
	return newError(a.waitFunc())
}

func (a *aggregator) filteredLinePipe(dst io.Writer, close func()) (int64, error) {
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
		buffered := len(line)

		for _, filter := range a.filterFuncs {
			var err error
			buffered, err = filter(a.ctx, line, &buf)
			if err != nil {
				return totalWritten, err
			}

			// No lines == skip
			if buffered == 0 {
				break
			}

			// Copy bytes and reset for the next filter
			line = make([]byte, buf.Len())
			copy(line, buf.Bytes())
			buf.Reset()
		}

		// If anything was written, treat it as a line and add a line ending for
		// convenience, unless it already has a line ending.
		if buffered > 0 && !bytes.HasSuffix(line, []byte("\n")) {
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
