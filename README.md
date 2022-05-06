# Design principles

- We don't case about stderr / stdout, we assume we never know, don't trust the tool.

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type FilterFunc func(string) CmdOut

type CmdOut struct {
	filters
}

func Cmd(ctx context.Context, args ...string) NewCmder {
	return nil
}

type NewCmder interface {
	WithInput(io.Reader) Cmd
	InDir(string) Cmd
	Env(map[string]string) Cmd
	Run() CmdOuptutter
	Raw() *exec.Command
}

type CmdOuptutter interface {
	StdErr() CmdOut // discard stdout
	StdOut() CmdOut // discard stderr

	Mode(mode string) CmdOut // add a side effect
	Filter(func(string) string) CmdOut

	Write(io.Writer) error
	Lines() ([]string, error)

	Err() CmdErr

	// Later
	Jq(string) CmdOut
}

func main2() {
	out := Cmd("gsutil blbaldbla").Filter(func(in string) { //
		// here
	})

	out := Cmd("gsutil blbaldbla").WithInput(StringsReader([]string{}))

	err, _ := out.Lines()
	CmdStderr(err) //
}

type CmdErr struct {
	exitError error
}

// Implements https://sourcegraph.com/github.com/urfave/cli/-/blob/errors.go?L79&subtree=true
func ExitCode() int { return 20 }

func (e *CmdErr) Error() string {
	return "exit status: 20 broken bread" // stderr, all of it yolo
}

func (e *CmdErr) StatusCode() int {
	return 20
}

func (e *CmdErr) Filter(func(string) string) error {
	return nil
}

func (e *CmdErr) Err() error {
	return nil
}

func ErrStatusCode(err error) int {
	return err.(*CmdErr).StatusCode()
}

func Cmd(args ...string) error {
	strings.Join(args, " ")
	return nil
}

func main() {

	cmd := Cmd("git checkout main")
	args := []string{"git checkout", "main"}
	cmd = Cmd(args...)
	// cmd = Cmdf("git checkout %", branch)

	str, err := pipe.Exec("").String()

	err := cmd.RunE()
	if err != nil {
		if cmd.Stderr() != "" {
			fmt.Println(cmd.Stderr())
		}
	}

	// quiet

	// normal: only see stdout

	// logging things +ls /foo/bar
	//                stderr ...

	cmd.Run().All().Mode(verbose).Filter().Write(os.Stdout)

	cmd.Out()
	cmd.CombinedOutput()

	cmd.Output()
	cmd.Stdout()
	err == cmd.Err() // exitCode or other errors

	out := cmd.Output() // CmdOutput
	out.Lines().Filter("bladblab").Replace("Try", "Dave")
	out.Replace()

}
```
