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
	if len(a.filterFuncs) == 0 {
		// Happy path, directly pipe output
		go io.Copy(dst, a.reader)
		return a.Wait()
	}

	// Pipe output via the filtered line pipe
	go a.filteredLinePipe(func(l []byte) { dst.Write(append(l, byte('\n'))) }, nil)
	return a.Wait()
}

func (a *aggregator) StreamLines(dst func(line []byte)) error {
	go a.filteredLinePipe(dst, nil)
	return a.Wait()
}

func (a *aggregator) Lines() ([]string, error) {
	// export lines
	linesC := make(chan string, 3)
	go a.filteredLinePipe(func(line []byte) {
		linesC <- string(line)
	}, func() { close(linesC) })

	// aggregate lines
	resultsC := make(chan []string, 1)
	defer close(resultsC)
	go func() {
		lines := make([]string, 0, 10)
		for line := range linesC {
			lines = append(lines, line)
		}
		resultsC <- lines
	}()

	// End command
	err := a.Wait()

	// Wait for results
	return <-resultsC, err
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

func (a *aggregator) Wait() error {
	if a.finalized {
		return errors.New("output aggregator has already been finalized")
	}
	a.finalized = true
	return newError(a.waitFunc())
}

func (a *aggregator) filteredLinePipe(send func([]byte), close func()) {
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

	if close != nil {
		close()
	}
}
