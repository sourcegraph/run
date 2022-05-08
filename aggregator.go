package run

import (
	"bufio"
	"bytes"
	"io"
)

// aggregator implements output finalization and formatting, and should be embedded in
// Output.
type aggregator struct {
	// reader is set to one of stdErr, stdOut, or both. It does not have filterFuncs
	// applied, they are applied at aggregation time.
	reader io.Reader

	// filterFuncs define line filters to be applied at aggregation time.
	filterFuncs []LineFilter

	// parentErr is encountered when building or starting to run a command - no output
	// should be handled if this is set
	parentErr error

	// wait is called before aggregation exit.
	wait func() (error, *bytes.Buffer)
}

// StreamTo writes filtered output from the command to the destination writer until
// command completion.
func (a *aggregator) Stream(dst io.Writer) error {
	if a.parentErr != nil {
		return newError(a.parentErr, nil)
	}

	if len(a.filterFuncs) == 0 {
		// Happy path, directly pipe output
		go io.Copy(dst, a.reader)
		return newError(a.wait())
	}

	// Pipe output via the filtered line pipe
	go a.filteredLinePipe(func(l []byte) { dst.Write(append(l, byte('\n'))) })
	return newError(a.wait())
}

// StreamLines writes filtered output from the command and sends it line by line to the
// destination callback until command completion.
func (a *aggregator) StreamLines(dst func(line []byte)) error {
	if a.parentErr != nil {
		return newError(a.parentErr, nil)
	}

	go a.filteredLinePipe(dst)
	return newError(a.wait())
}

// Lines waits for command completion and aggregates filtered output from the command.
func (a *aggregator) Lines() ([]string, error) {
	if a.parentErr != nil {
		return nil, newError(a.parentErr, nil)
	}

	lines := make([]string, 0, 10)
	go a.filteredLinePipe(func(line []byte) {
		lines = append(lines, string(line))
	})
	return lines, newError(a.wait())
}

// Wait waits for command completion and returns.
func (a *aggregator) Wait() error {
	if a.parentErr != nil {
		return newError(a.parentErr, nil)
	}

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
