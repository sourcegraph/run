package run

import (
	"bufio"
	"bytes"
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

	// wait is called before aggregation exit.
	wait func() (error, *bytes.Buffer)
}

func (a *aggregator) Stream(dst io.Writer) error {
	if len(a.filterFuncs) == 0 {
		// Happy path, directly pipe output
		go io.Copy(dst, a.reader)
		return newError(a.wait())
	}

	// Pipe output via the filtered line pipe
	go a.filteredLinePipe(func(l []byte) { dst.Write(append(l, byte('\n'))) })
	return newError(a.wait())
}

func (a *aggregator) StreamLines(dst func(line []byte)) error {
	go a.filteredLinePipe(dst)
	return newError(a.wait())
}

func (a *aggregator) Lines() ([]string, error) {
	lines := make([]string, 0, 10)
	go a.filteredLinePipe(func(line []byte) {
		lines = append(lines, string(line))
	})
	return lines, newError(a.wait())
}

func (a *aggregator) Wait() error {
	return newError(a.wait())
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
