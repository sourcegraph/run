package run_test

import (
	"context"
	"fmt"

	"github.com/sourcegraph/run"
)

func ExampleExitCode() {
	ctx := context.Background()

	err := run.Bash(ctx, "exit 123").Run().Wait()
	fmt.Println(run.ExitCode(err))

	err = run.Cmd(ctx, "echo", run.Arg("hello world!")).Run().Wait()
	fmt.Println(run.ExitCode(err))

	err = run.Cmd(ctx, "non-existing-binary").Run().Wait()
	fmt.Println(run.ExitCode(err))

	// Output:
	// 123
	// 0
	// 1
}
