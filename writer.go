package run

import (
	"bufio"
	"bytes"
	"io"
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
