package run

import (
	"bytes"
	"context"
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestLargeOutput(t *testing.T) {
	c := qt.New(t)

	// Set buffer size to very small so we can trigger limits easily
	defaultMaxBufferSize := maxBufferSize
	maxBufferSize = 1024
	c.Cleanup(func() { maxBufferSize = defaultMaxBufferSize })

	// Use LICENSE as the large output for testing:
	//
	// 	$ wc -c "LICENSE" | awk '{print $1}'
	// 	11347
	//
	// If LICENSE changes, this test may need to be updated.
	var out bytes.Buffer
	err := Cmd(context.Background(), "cat ./LICENSE").Run().Stream(&out)
	c.Assert(err, qt.IsNil)
	c.Assert(out.Len(), qt.Equals, 11347, qt.Commentf("Only got %d bytes", out.Len()))
}
