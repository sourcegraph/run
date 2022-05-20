package run

import (
	"bytes"
	"context"
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
}
