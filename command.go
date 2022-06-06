package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"bitbucket.org/creachadair/shell"
)

// Command builds a command for execution. Functions modify the underlying command.
type Command struct {
	ctx context.Context

	args    []string
	environ []string
	dir     string
	stdin   io.Reader
	attach  attachedOuput

	// buildError represents an error that occured when building this command.
	buildError error
}

// Cmd joins all the parts and builds a command from it.
//
// Arguments are not implicitly quoted - to quote arguemnts, you can use Arg.
func Cmd(ctx context.Context, parts ...string) *Command {
	args, ok := shell.Split(strings.Join(parts, " "))
	if !ok {
		return &Command{buildError: errors.New("provided parts has unclosed quotes")}
	}

	return &Command{
		ctx:  ctx,
		args: args,
	}
}

// Bash joins all the parts and builds a command from it to be run by 'bash -c'.
//
// Arguments are not implicitly quoted - to quote arguemnts, you can use Arg.
func Bash(ctx context.Context, parts ...string) *Command {
	return Cmd(ctx, "bash -c", Arg(strings.Join(parts, " ")))
}

// Run starts command execution and returns Output, which defaults to combined output.
func (c *Command) Run() Output {
	if c.buildError != nil {
		return NewErrorOutput(c.buildError)
	}
	if len(c.args) == 0 {
		return NewErrorOutput(errors.New("Command not instantiated"))
	}

	cmd := exec.CommandContext(c.ctx, c.args[0], c.args[1:]...)
	cmd.Dir = c.dir
	cmd.Stdin = c.stdin
	cmd.Env = c.environ
	return attachOutputAndRun(c.ctx, c.attach, cmd)
}

// Dir sets the directory this command should be executed in.
func (c *Command) Dir(dir string) *Command {
	c.dir = dir
	return c
}

// Input pipes the given io.Reader to the command. If an input is already set, the given
// input is appended.
func (c *Command) Input(input io.Reader) *Command {
	if c.stdin != nil {
		c.stdin = io.MultiReader(c.stdin, input)
	} else {
		c.stdin = input
	}
	return c
}

// ResetInput sets the command's input to nil.
func (c *Command) ResetInput() *Command {
	c.stdin = nil
	return c
}

// Env adds the given environment variables to the command.
func (c *Command) Env(env map[string]string) *Command {
	for k, v := range env {
		c.environ = append(c.environ, fmt.Sprintf("%s=%s", k, v))
	}
	return c
}

// Environ adds the given strings representing the environment (key=value) to the
// command, for example os.Environ().
func (c *Command) Environ(environ []string) *Command {
	c.environ = append(c.environ, environ...)
	return c
}

// StdOut configures the command Output to only provide StdOut. By default, Output
// includes combined output.
func (c *Command) StdOut() *Command {
	c.attach = attachOnlyStdOut
	return c
}

// StdErr configures the command Output to only provide StdErr. By default, Output
// includes combined output.
func (c *Command) StdErr() *Command {
	c.attach = attachOnlyStdErr
	return c
}
