# 🏃‍♂️ run

A new way to execute commands in Go

## Example usage

<!-- START EXAMPLE -->

```go
package main

import (
  "bytes"
  "context"
  "fmt"
  "log"
  "os"

  "github.com/sourcegraph/run"
)

func main() {
  ctx := context.Background()

  // Easily stream all output back to standard out
  err := run.Cmd(ctx, "echo", "hello world").Run().Stream(os.Stdout)
  if err != nil {
    log.Fatal(err.Error())
  }

  // Or collect filter and modify standard out, then collect string lines from it
  lines, err := run.Cmd(ctx, "ls").Run().
    Filter(func(s []byte) ([]byte, bool) {
      if !bytes.HasSuffix(s, []byte(".go")) {
        return nil, true
      }
      return bytes.TrimSuffix(s, []byte(".go")), false
    }).
    Lines()
  if err != nil {
    log.Fatal(err.Error())
  }
  for i, l := range lines {
    fmt.Printf("line %d: %q\n", i, l)
  }

  // Errors include standard error by default, so we can just stream stdout.
  err = run.Cmd(ctx, "ls", "foobar").Run().StdOut().Stream(os.Stdout)
  if err != nil {
    println(err.Error()) // exit status 1: ls: foobar: No such file or directory
  }

  // Generate data from a file, replacing tabs with spaces for Markdown purposes
  var exampleData bytes.Buffer
  exampleData.Write([]byte(exampleStart + "\n\n```go\n"))
  if err = run.Cmd(ctx, "cat", "cmd/runexample/main.go").Run().
    Filter(func(line []byte) ([]byte, bool) {
      return bytes.ReplaceAll(line, []byte("\t"), []byte("  ")), false
    }).
    Stream(&exampleData); err != nil {
    log.Fatal(err)
  }
  exampleData.Write([]byte("```\n\n" + exampleEnd))

  // Render new README file
  var readmeData bytes.Buffer
  if err = run.Cmd(ctx, "cat", "README.md").Run().Stream(&readmeData); err != nil {
    log.Fatal(err)
  }
  replaced := exampleBlockRegexp.ReplaceAll(readmeData.Bytes(), exampleData.Bytes())

  // Pipe data to command
  err = run.Cmd(ctx, "cp /dev/stdin README.md").WithInput(bytes.NewReader(replaced)).Run().Wait()
  if err != nil {
    log.Fatal(err)
  }
}
```

<!-- END EXAMPLE -->
