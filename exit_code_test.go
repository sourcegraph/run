package run_test

import (
	"context"
	"fmt"

	"bitbucket.org/creachadair/shell"
	"github.com/sourcegraph/run"
)

func ExampleExitCode() {
	ctx := context.Background()

	err := run.Cmd(ctx, "bash -c", shell.Quote("exit 123")).Run().Wait()
	fmt.Println(run.ExitCode(err))

	err = run.Cmd(ctx, "echo 'hello world!'").Run().Wait()
	fmt.Println(run.ExitCode(err))

	err = run.Cmd(ctx, "non-existing-binary").Run().Wait()
	fmt.Println(run.ExitCode(err))
	// Output:
	// 123
	// 0
	// 1
}
