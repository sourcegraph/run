# AGENTS.md

## Overview

`github.com/sourcegraph/run` is a Go library for executing commands, providing a
fluent API for running processes and streaming, mapping, and collecting their
output. It has no external runtime services; it is consumed as a package.

## Layout

- Library source lives in package `run` at the repository root (`command.go`,
  `output.go`, `map.go`, `jq.go`, `instrumentation.go`, etc.).
- `cmd/` holds runnable examples (`example`, `jqexample`, `pipeexample`,
  `pollexample`). `cmd/example/main.go` also regenerates the README example block.
- Tests are `*_test.go` files alongside the code they cover.

## Setup

- Go 1.25 (CI pins `1.25.9`). Module: `github.com/sourcegraph/run`.
- Fetch dependencies:

```bash
go mod download
```

## Build, test, lint

These mirror `.github/workflows/pipeline.yml`:

```bash
go build -v ./...
go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
go vet ./...
gofmt -l .
```

## Conventions

- Standard Go formatting (`gofmt`); keep code idiomatic to the existing files.
- Tests use `github.com/frankban/quicktest`.
- Commands take a `context.Context`; the fluent API chains `Cmd(...).Run()` with
  output operations like `Stream`, `Lines`, and `Map`.
- See `CODEOWNERS` for review ownership.
