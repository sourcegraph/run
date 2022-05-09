package run_test

import (
	"context"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/sourcegraph/run"
)

func TestRun(t *testing.T) {
	c := qt.New(t)

	c.Run(`echo "hello world"`, func(c *qt.C) {
		// Easily stream all output back to standard out
		lines, err := run.Cmd(context.Background(), "echo", "hello world").Run().Lines()
		c.Assert(err, qt.IsNil)
		c.Assert(len(lines), qt.Equals, 1)
		c.Assert(lines[0], qt.Equals, "hello world")
	})
}
