package run

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// aggregator implements output finalization and formatting, and should be embedded in
// commandOutput to fulfill the Output interface.
type aggregator struct {
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
	if len(a.filterFuncs) == 0 {
		// Happy path, directly pipe output
		go io.Copy(dst, a.reader)
		return a.Wait()
	}

	// Pipe output via the filtered line pipe
	go a.filteredLinePipe(func(l []byte) { dst.Write(append(l, byte('\n'))) })
	return a.Wait()
}

func (a *aggregator) StreamLines(dst func(line []byte)) error {
	go a.filteredLinePipe(dst)
	return a.Wait()
}

func (a *aggregator) Lines() ([]string, error) {
	lines := make([]string, 0, 10)
	go a.filteredLinePipe(func(line []byte) {
		lines = append(lines, string(line))
	})
	return lines, a.Wait()
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

func (a *aggregator) Wait() error {
	if a.finalized {
		return errors.New("output aggregator has already been finalized")
	}
	a.finalized = true
	return newError(a.waitFunc())
}

func (a *aggregator) filteredLinePipe(send func([]byte)) {
	scanner := bufio.NewScanner(a.reader)

	// TODO should we introduce API for configuring max capacity?
	// Errors will happen with lengths > 65536
	// const maxCapacity int = longLineLen
	// buf := make([]byte, maxCapacity)
	// scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		line := scanner.Bytes()

		var skip bool
		for _, filter := range a.filterFuncs {
			line, skip = filter(line)
			if skip {
				break
			}
		}

		if !skip {
			send(line)
		}
	}
}