package main

import (
	"context"
	"log"
	"os"

	"github.com/sourcegraph/run"
)

func main() {
	ctx := context.Background()

	// Demonstrate that output streams live!
	cmd := run.Bash(ctx, `for i in {1..10}; do echo -n "This is a test in loop $i "; date ; sleep 1; done`)
	if err := cmd.Run().Stream(os.Stdout); err != nil {
		log.Fatal(err)
	}
}
