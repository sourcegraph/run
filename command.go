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
	// cmd is the underlying exec.Cmd that carries the command execution.
	cmd *exec.Cmd
	// out configures command output.
	out Output
	// buildError represents an error that occured when building this command.
	buildError error
}

// Cmd joins all the parts and builds a command from it.
func Cmd(ctx context.Context, parts ...string) *Command {
	params, ok := shell.Split(strings.Join(parts, " "))
	if !ok {
		return &Command{buildError: errors.New("provided args are invalid")}
	}

	return &Command{
		ctx: ctx,
		cmd: exec.CommandContext(ctx, params[0], params[1:]...),
	}
}

// Bash joins all the parts and builds a command from it to be run by 'bash -c'.
func Bash(ctx context.Context, parts ...string) *Command {
	return Cmd(ctx, "bash -c", shell.Quote(strings.Join(parts, " ")))
}

// Run starts command execution and returns Output, which defaults to combined output.
func (c *Command) Run() Output {
	if c.buildError != nil {
		return NewErrorOutput(c.buildError)
	}
	if c.cmd == nil {
		return NewErrorOutput(errors.New("Command not instantiated"))
	}

	return attachOutputAndRun(c.ctx, c.cmd)
}

// Dir sets the directory this command should be executed in.
func (c *Command) Dir(dir string) *Command {
	if c.cmd == nil {
		return c
	}

	c.cmd.Dir = dir
	return c
}

// Input pipes the given io.Reader to the command. If an input is already set, the given
// input is appended.
func (c *Command) Input(input io.Reader) *Command {
	if c.cmd == nil {
		return c
	}

	if c.cmd.Stdin != nil {
		c.cmd.Stdin = io.MultiReader(c.cmd.Stdin, input)
	} else {
		c.cmd.Stdin = input
	}
	return c
}

// ResetInput sets the command's input to nil.
func (c *Command) ResetInput() *Command {
	if c.cmd == nil {
		return c
	}

	c.cmd.Stdin = nil
	return c
}

// Env adds the given environment variables to the command.
func (c *Command) Env(env map[string]string) *Command {
	if c.cmd == nil {
		return c
	}

	for k, v := range env {
		c.cmd.Env = append(c.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	return c
}

// InheritEnv adds the given strings representing the environment (key=value) to the
// command, for example os.Environ().
func (c *Command) Environ(environ []string) *Command {
	if c.cmd == nil {
		return c
	}

	c.cmd.Env = append(c.cmd.Env, environ...)
	return c
}
