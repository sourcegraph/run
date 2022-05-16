package run

import (
	"context"
	"io"
)

// LineFilter allows modifications of individual lines from Output and enables callbacks
// that operate on lines from Output.
//
// The return value mirrors the signature of (Writer).Write(), and should be used to
// indicate what was written to dst.
type LineFilter func(ctx context.Context, line []byte, dst io.Writer) (int, error)

// JQFilter creates a LineFilter that executes a JQ query against each line. Errors at
// runtime get written to output.
//
// Refer to https://github.com/itchyny/gojq for the specifics of supported syntax.
func JQFilter(query string) (LineFilter, error) {
	jqCode, err := buildJQ(query)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
		b, err := execJQ(ctx, jqCode, line)
		if err != nil {
			return 0, err
		}
		return dst.Write(b)
	}, nil
}
