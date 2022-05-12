package run_test

import (
	"context"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/sourcegraph/run"
)

func TestOutput(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	c.Run("output.JQ", func(c *qt.C) {
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
