package main

import (
	"context"
	"log"
	"os"

	"github.com/sourcegraph/run"
)

func main() {
	ctx := context.Background()

	// Run command and get Output
	lsOut := run.Cmd(ctx, "ls cmd").Run().
		Filter(func(line []byte) ([]byte, bool) {
			return append([]byte("./cmd/"), line...), false
		})

	// Pipe Output directly to another command!
	err := run.Cmd(ctx, "cat").Input(lsOut).Run().
		Stream(os.Stdout)
	if err != nil {
		log.Fatal(err.Error())
	}
}
