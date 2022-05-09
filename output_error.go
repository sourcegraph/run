package run

import "io"

type errorOutput struct{ err error }

// newErrorOutput creates an Output that just returns error.
func newErrorOutput(err error) Output { return &errorOutput{err: err} }

func (o *errorOutput) StdErr() Output                  { return o }
func (o *errorOutput) StdOut() Output                  { return o }
func (o *errorOutput) Filter(filter LineFilter) Output { return o }

func (o *errorOutput) Stream(dst io.Writer) error              { return o.err }
func (o *errorOutput) StreamLines(dst func(line []byte)) error { return o.err }
func (o *errorOutput) Lines() ([]string, error)                { return nil, o.err }
func (o *errorOutput) Wait() error                             { return o.err }
