package run

import "io"

type errorOutput struct{ err error }

// NewErrorOutput creates an Output that just returns error. Useful for allowing function
// that help run Commands and want to just return an Output even if errors can happen
// before command execution.
func NewErrorOutput(err error) Output { return &errorOutput{err: err} }

func (o *errorOutput) StdErr() Output                  { return o }
func (o *errorOutput) StdOut() Output                  { return o }
func (o *errorOutput) Filter(filter LineFilter) Output { return o }

func (o *errorOutput) Stream(dst io.Writer) error              { return o.err }
func (o *errorOutput) StreamLines(dst func(line []byte)) error { return o.err }
func (o *errorOutput) Lines() ([]string, error)                { return nil, o.err }
func (o *errorOutput) JQ(query string) ([]byte, error)         { return nil, o.err }
func (o *errorOutput) Read(p []byte) (int, error)              { return 0, o.err }
func (o *errorOutput) WriteTo(io.Writer) (int64, error)        { return 0, o.err }

func (o *errorOutput) Wait() error { return o.err }
