package run

import (
	"context"
	"fmt"

	"bitbucket.org/creachadair/shell"
)

func ExampleExitCode() {
	ctx := context.Background()

	err := Cmd(ctx, "bash -c", shell.Quote("exit 123")).Run().Wait()
	fmt.Println(ExitCode(err))

	err = Cmd(ctx, "echo 'hello world!'").Run().Wait()
	fmt.Println(ExitCode(err))

	err = Cmd(ctx, "non-existing-binary").Run().Wait()
	fmt.Println(ExitCode(err))
	// Output:
	// 123
	// 0
	// 1
}
