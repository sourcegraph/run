package run_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/sourcegraph/run"
)

var outputTests = []func(c *qt.C, out run.Output, expect string){
	func(c *qt.C, out run.Output, expect string) {
		c.Run("Stream", func(c *qt.C) {
			var b bytes.Buffer
			err := out.Stream(&b)
			c.Assert(err, qt.IsNil)
			c.Assert(b.String(), qt.Equals, fmt.Sprintf("%s\n", expect))
		})
	},
	func(c *qt.C, out run.Output, expect string) {
		c.Run("StreamLines", func(c *qt.C) {
			linesC := make(chan []byte, 10)
			err := out.StreamLines(func(line []byte) {
				linesC <- line
			})
			c.Assert(err, qt.IsNil)
			close(linesC)

			var lines [][]byte
			for l := range linesC {
				lines = append(lines, l)
			}
			c.Assert(len(lines), qt.Equals, 1)
			c.Assert(string(lines[0]), qt.Equals, expect)
		})
	},
	func(c *qt.C, out run.Output, expect string) {
		c.Run("Lines", func(c *qt.C) {
			lines, err := out.Lines()
			c.Assert(err, qt.IsNil)
			c.Assert(len(lines), qt.Equals, 1)
			c.Assert(lines[0], qt.Equals, expect)
		})
	},
	func(c *qt.C, out run.Output, expect string) {
		c.Run("String", func(c *qt.C) {
			str, err := out.String()
			c.Assert(err, qt.IsNil)
			c.Assert(str, qt.Equals, expect)
		})
	},
	func(c *qt.C, out run.Output, expect string) {
		c.Run("Read: fixed bytes", func(c *qt.C) {
			b := make([]byte, 100)
			n, err := out.Read(b)
			c.Assert(err, qt.IsNil)
			c.Assert(string(b[0:n]), qt.Equals, fmt.Sprintf("%s\n", expect))
		})
	},
	func(c *qt.C, out run.Output, expect string) {
		c.Run("Read: exactly length of output", func(c *qt.C) {
			// Read exactly the amount of output
			b := make([]byte, len(expect)+1)
			n, err := out.Read(b)
			c.Assert(err, qt.IsNil)
			c.Assert(string(b[0:n]), qt.Equals, fmt.Sprintf("%s\n", expect))

			// A subsequent read should indicate nothing read, and an EOF
			n, err = out.Read(make([]byte, 100))
			c.Assert(n, qt.Equals, 0)
			c.Assert(err, qt.Equals, io.EOF)
		})
	},
	func(c *qt.C, out run.Output, expect string) {
		c.Run("Read: io.ReadAll", func(c *qt.C) {
			b, err := io.ReadAll(out)
			c.Assert(err, qt.IsNil)
			c.Assert(string(b), qt.Equals, fmt.Sprintf("%s\n", expect))
		})
	},
	func(c *qt.C, out run.Output, expect string) {
		c.Run("Wait", func(c *qt.C) {
			err := out.Wait()
			c.Assert(err, qt.IsNil)
		})
	},
}

func TestRunAndAggregate(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	command := `echo "hello world"`

	type testCase struct {
		name   string
		output func() run.Output
		expect string
	}
	for _, tc := range []testCase{
		{
			name: "plain output",
			output: func() run.Output {
				return run.Cmd(ctx, command).Run()
			},
			expect: "hello world",
		},
		{
			name: "mapped output",
			output: func() run.Output {
				return run.Cmd(ctx, command).Run().
					Map(func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
						return dst.Write(bytes.ReplaceAll(line, []byte("hello"), []byte("goodbye")))
					})
			},
			expect: "goodbye world",
		},
		{
			name: "multiple mapped output",
			output: func() run.Output {
				return run.Cmd(ctx, command).Run().
					Map(func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
						return dst.Write(bytes.ReplaceAll(line, []byte("hello"), []byte("goodbye")))
					}).
					Map(func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
						return dst.Write(bytes.ReplaceAll(line, []byte("world"), []byte("jh")))
					})
			},
			expect: "goodbye jh",
		},
	} {
		c.Run(tc.name, func(c *qt.C) {
			for _, test := range outputTests {
				test(c, tc.output(), tc.expect)
			}
		})
	}
}

func TestJQ(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

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

}

func TestEdgeCases(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

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
