package run

import "context"

// LineFilter allows modifications of individual lines from Output.
//
// An explicit "skip" return parameter is required because many bytes library functions
// return nil to denote empty lines, which should be preserved: https://github.com/golang/go/issues/46415
type LineFilter func(line []byte) (newLine []byte, skip bool)

// JQFilter creates a LineFilter that executes a JQ query against each line. Errors at
// runtime get written to output.
//
// Refer to https://github.com/itchyny/gojq for the specifics of supported syntax.
func JQFilter(query string) (LineFilter, error) {
	jqCode, err := buildJQ(query)
	if err != nil {
		return nil, err
	}

	return func(line []byte) ([]byte, bool) {
		b, err := execJQ(context.TODO(), jqCode, line)
		if err != nil {
			return []byte(err.Error()), false
		}
		return b, false
	}, nil
}
