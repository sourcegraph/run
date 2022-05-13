package run_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/sourcegraph/run"
)

func TestRunAndAggregate(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	command := `echo "hello world"`
	c.Run(command, func(c *qt.C) {
		c.Run("Lines", func(c *qt.C) {
			lines, err := run.Cmd(ctx, command).Run().Lines()
			c.Assert(err, qt.IsNil)
			c.Assert(len(lines), qt.Equals, 1)
			c.Assert(lines[0], qt.Equals, "hello world")
		})

		c.Run("Stream", func(c *qt.C) {
			var b bytes.Buffer
			err := run.Cmd(ctx, command).Run().Stream(&b)
			c.Assert(err, qt.IsNil)
			c.Assert(b.String(), qt.Equals, "hello world\n")
		})

		c.Run("StreamLines", func(c *qt.C) {
			var lines [][]byte
			err := run.Cmd(ctx, command).Run().StreamLines(func(line []byte) {
				lines = append(lines, line)
			})
			c.Assert(err, qt.IsNil)
			c.Assert(len(lines), qt.Equals, 1)
			c.Assert(string(lines[0]), qt.Equals, "hello world")
		})

		c.Run("JQ", func(c *qt.C) {
			const testJSON = `{
				"hello": "world"		
			}`

			res, err := run.Cmd(ctx, "cat").
				Input(strings.NewReader(testJSON)).
				Run().
				JQ(".hello")
			c.Assert(err, qt.IsNil)
			c.Assert(string(res), qt.Equals, `"world"`)
		})

		c.Run("Read", func(c *qt.C) {
			b := make([]byte, 100)
			n, err := run.Cmd(ctx, command).Run().Read(b)
			c.Assert(err, qt.IsNil)
			c.Assert(string(b[0:n-1]), qt.Equals, "hello world\n")
		})

		c.Run("Wait", func(c *qt.C) {
			err := run.Cmd(ctx, command).Run().Wait()
			c.Assert(err, qt.IsNil)
		})
	})
}

func TestInput(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	c.Run("set multiple inputs", func(c *qt.C) {
		cmd := run.Cmd(ctx, "cat").
			Input(strings.NewReader("hello")).
			Input(strings.NewReader(" ")).
			Input(strings.NewReader("world\n"))

		lines, err := cmd.Run().Lines()
		c.Assert(err, qt.IsNil)
		c.Assert(lines, qt.Equals, []string{"hello world"})
	})

	c.Run("reset input", func(c *qt.C) {
		cmd := run.Cmd(ctx, "cat").
			Input(strings.NewReader("hello")).
			ResetInput().
			Input(strings.NewReader("world"))

		lines, err := cmd.Run().Lines()
		c.Assert(err, qt.IsNil)
		c.Assert(lines, qt.Equals, []string{"world"})
	})
}
