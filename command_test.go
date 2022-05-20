package run_test

import (
	"bytes"
	"context"
	"io"
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
		c.Run("Stream", func(c *qt.C) {
			var b bytes.Buffer
			err := run.Cmd(ctx, command).Run().Stream(&b)
			c.Assert(err, qt.IsNil)
			c.Assert(b.String(), qt.Equals, "hello world\n")
		})

		c.Run("StreamLines", func(c *qt.C) {
			linesC := make(chan []byte, 10)
			err := run.Cmd(ctx, command).Run().StreamLines(func(line []byte) {
				linesC <- line
			})
			c.Assert(err, qt.IsNil)
			close(linesC)

			var lines [][]byte
			for l := range linesC {
				lines = append(lines, l)
			}
			c.Assert(len(lines), qt.Equals, 1)
			c.Assert(string(lines[0]), qt.Equals, "hello world")
		})

		c.Run("Lines", func(c *qt.C) {
			lines, err := run.Cmd(ctx, command).Run().Lines()
			c.Assert(err, qt.IsNil)
			c.Assert(len(lines), qt.Equals, 1)
			c.Assert(lines[0], qt.Equals, "hello world")
		})

		c.Run("String", func(c *qt.C) {
			str, err := run.Cmd(ctx, command).Run().String()
			c.Assert(err, qt.IsNil)
			c.Assert(str, qt.Equals, "hello world")
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

	c.Run("cat and JQ", func(c *qt.C) {
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

	c.Run("empty lines from map are preserved", func(c *qt.C) {
		const testData = `hello
		
		world`

		c.Run("without map", func(c *qt.C) {
			res, err := run.Cmd(ctx, "cat").
				Input(strings.NewReader(testData)).
				Run().
				Lines()
			c.Assert(err, qt.IsNil)
			c.Assert(len(res), qt.Equals, 3)
		})

		c.Run("with map", func(c *qt.C) {
			res, err := run.Cmd(ctx, "cat").
				Input(strings.NewReader(testData)).
				Run().
				Map(func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
					return dst.Write(line)
				}).
				Lines()
			c.Assert(err, qt.IsNil)
			c.Assert(len(res), qt.Equals, 3)
		})
	})

	c.Run("mixed output", func(c *qt.C) {
		const mixedOutputCmd = `echo "stdout" ; sleep 0.001 ; >&2 echo "stderr"`

		c.Run("stdout only", func(c *qt.C) {
			res, err := run.Bash(ctx, mixedOutputCmd).
				StdOut().
				Run().
				Lines()
			c.Assert(err, qt.IsNil)
			c.Assert(res, qt.CmpEquals(), []string{"stdout"})
		})

		c.Run("stderr only", func(c *qt.C) {
			res, err := run.Bash(ctx, mixedOutputCmd).
				StdErr().
				Run().
				Lines()
			c.Assert(err, qt.IsNil)
			c.Assert(res, qt.CmpEquals(), []string{"stderr"})
		})

		c.Run("combined", func(c *qt.C) {
			res, err := run.Bash(ctx, mixedOutputCmd).
				Run().
				Lines()
			c.Assert(err, qt.IsNil)
			c.Assert(res, qt.CmpEquals(), []string{"stdout", "stderr"})
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
		c.Assert(lines, qt.CmpEquals(), []string{"hello world"})
	})

	c.Run("reset input", func(c *qt.C) {
		cmd := run.Cmd(ctx, "cat").
			Input(strings.NewReader("hello")).
			ResetInput().
			Input(strings.NewReader("world"))

		lines, err := cmd.Run().Lines()
		c.Assert(err, qt.IsNil)
		c.Assert(lines, qt.CmpEquals(), []string{"world"})
	})
}
