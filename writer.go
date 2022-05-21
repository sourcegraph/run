package run

import (
	"bufio"
	"bytes"
	"io"

	"github.com/djherbis/nio/v3"
)

type lineWriter struct {
	handler func([]byte)
}

func newLineWriter(handler func([]byte)) io.Writer {
	return &lineWriter{handler: handler}
}

func (lw *lineWriter) Write(b []byte) (int, error) {
	n := len(b)

	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		lw.handler(scanner.Bytes())
	}

	return n, nil
}

type tracedBuffer struct {
	// writeCalled indicates that Write was called at all, even with empty input.
	writeCalled bool

	*bytes.Buffer
}

func (t *tracedBuffer) Write(b []byte) (int, error) {
	t.writeCalled = true
	return t.Buffer.Write(b)
}

// closerWithError is part of the buffer pipe writer interface for closing the pipe.
type closerWithError interface {
	CloseWithError(error) error
}

var _ closerWithError = &nio.PipeWriter{}
