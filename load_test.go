package run

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestLargeOutput(t *testing.T) {
	c := qt.New(t)

	// Use LICENSE as the large output for testing
	largeFile := "./LICENSE"
	largeOutputContents, err := os.ReadFile(largeFile)
	c.Assert(err, qt.IsNil)

	var runLargeOutputCommand = func() Output {
		return Cmd(context.Background(), "cat", largeFile).Run()
	}

	c.Run("buffer limits", func(c *qt.C) {
		// Set buffer size to very small so we can trigger limits easily
		defaultMaxBufferSize := maxBufferSize
		maxBufferSize = 1024
		c.Cleanup(func() { maxBufferSize = defaultMaxBufferSize })

		var out bytes.Buffer
		err := runLargeOutputCommand().Stream(&out)
		c.Assert(err, qt.IsNil)
		c.Assert(out.String(), qt.Equals, string(largeOutputContents),
			qt.Commentf("Only got %d bytes", out.Len()))
	})

	c.Run("multi-part read", func(c *qt.C) {
		output := runLargeOutputCommand()

		// try to read the entire file
		var out bytes.Buffer
		for out.Len() < len(largeOutputContents) {
			// read a chunk of a file.
			b := make([]byte, 1024)
			n, err := output.Read(b)
			c.Assert(err, qt.IsNil)

			// track the entire output
			out.Write(b[:n])
		}

		c.Assert(out.String(), qt.Equals, string(largeOutputContents),
			qt.Commentf("Only got %d bytes", out.Len()))
	})

	c.Run("mapped read", func(c *qt.C) {
		var (
			oldLicense = []byte("License")
			newLicense = []byte("Robert")

			oldSourcegraph = []byte("Sourcegraph")
			newSourcegraph = []byte("Horsegraph")
		)
		output := runLargeOutputCommand().
			Map(func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
				return dst.Write(bytes.ReplaceAll(line, oldLicense, newLicense))
			}).
			Map(func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
				return dst.Write(bytes.ReplaceAll(line, oldSourcegraph, newSourcegraph))
			})

		// Test the reader implementation
		data, err := io.ReadAll(output)
		c.Assert(err, qt.IsNil)

		// We should have roughly all the data here
		wantLowerBound := len(largeOutputContents) * 8 / 10
		c.Assert(len(data) > wantLowerBound, qt.IsTrue,
			qt.Commentf("Only got %d bytes, wanted more than %d", len(data), wantLowerBound))

		c.Assert(string(data), qt.Contains, string(newLicense))
		c.Assert(bytes.Contains(data, oldLicense), qt.IsFalse)
		c.Assert(string(data), qt.Contains, string(newSourcegraph))
		c.Assert(bytes.Contains(data, oldSourcegraph), qt.IsFalse)
	})
}
