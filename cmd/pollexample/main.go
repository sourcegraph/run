package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"

	"github.com/sourcegraph/run"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	// Demonstrate that output streams live!
	cmd := run.Bash(ctx, `for i in {1..10}; do echo -n "This is a test in loop $i "; date ; sleep 1; done`)
	if err := cmd.Run().
		Map(func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
			if bytes.Contains(line, []byte("loop 3")) {
				defer cancel() // Interrupt parent context here

				written, err := run.Cmd(ctx, "echo", "Loop 3 detected, running a subcommand!").
					Run().
					WriteTo(dst)
				return int(written), err
			}

			return dst.Write(line)
		}).
		Stream(os.Stdout); err != nil {
		log.Fatal(err)
	}
}
