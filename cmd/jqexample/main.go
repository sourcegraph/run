package main

import (
	"context"
	"log"
	"os"

	"github.com/sourcegraph/run"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Expected jq argument")
	}

	ctx := context.Background()
	res, err := run.Cmd(ctx, "cat").Input(os.Stdin).Run().JQ(os.Args[1])
	if err != nil {
		log.Fatal(err.Error())
	}
	println(string(res))
}
