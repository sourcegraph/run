package main

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/sourcegraph/run"
)

func main() {
	ctx := context.Background()

	// Run command and get Output
	lsOut := run.Cmd(ctx, "ls cmd").Run().
		Filter(func(ctx context.Context, line []byte, dst io.Writer) (int, error) {
			return dst.Write(append([]byte("./cmd/"), line...))
		})

	// Pipe Output directly to another command!
	err := run.Cmd(ctx, "cat").Input(lsOut).Run().
		Stream(os.Stdout)
	if err != nil {
		log.Fatal(err.Error())
	}
}
