package run_test

import (
	"context"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/sourcegraph/run"
)

func TestJQMap(t *testing.T) {
	c := qt.New(t)
	ctx := context.Background()

	const jsonLines = `{"msg":"hello world"}
{"msg":"hello jh"}
{"msg":"hi robert!"}
`

	jqMap, err := run.MapJQ(".msg")
	c.Assert(err, qt.IsNil)

	lines, err := run.Cmd(ctx, "cat").
		Input(strings.NewReader(jsonLines)).
		Run().
		Map(jqMap).
		Lines()
	c.Assert(err, qt.IsNil)
	c.Assert(lines, qt.CmpEquals(), []string{
		`"hello world"`,
		`"hello jh"`,
		`"hi robert!"`,
	})
}
