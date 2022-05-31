package run

import (
	"bufio"
	"bytes"
	"context"
	"io"
)

// LineMap allows modifications of individual lines from Output and enables callbacks
// that operate on lines from Output. Bytes written to dst are collected and passed to
// subsequent LineMaps before being written to output aggregation, e.g. Output.Stream().
//
// The return value mirrors the signature of (Writer).Write(), and should be used to
// indicate what was written to dst.
//
// Errors interrupt line processing and are returned if and only if the command itself
// did not exit with an error.
type LineMap func(ctx context.Context, line []byte, dst io.Writer) (int, error)

// MapJQ creates a LineMap that executes a JQ query against each line and replaces the
// output with the result.
//
// Refer to https://github.com/itchyny/gojq for the specifics of supported syntax.
func MapJQ(query string) (LineMap, error) {
	jqCode, err := buildJQ(query)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
		b, err := execJQBytes(ctx, jqCode, line)
		if err != nil {
			return 0, err
		}
		return dst.Write(b)
	}, nil
}

type lineMaps []LineMap

// Pipe applies lineMaps sequentially to dst from src, and returns the number of bytes
// read.
func (m lineMaps) Pipe(ctx context.Context, src io.Reader, dst io.Writer, close func()) (int64, error) {
	if close != nil {
		defer close()
	}

	scanner := bufio.NewScanner(src)

	var buf bytes.Buffer
	var totalWritten int64
	for scanner.Scan() {
		line := scanner.Bytes()

		// Defaults to true because if no map funcs unset this, then we will write the
		// entire line.
		writeCalled := true

		for _, f := range m {
			tb := &tracedBuffer{Buffer: &buf}
			buffered, err := f(ctx, line, tb)
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
