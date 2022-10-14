package run_test

import (
	"context"
	"io"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/sourcegraph/run"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInstrumentation(t *testing.T) {
	c := qt.New(t)

	c.Run("Logging", func(c *qt.C) {
		// Enable logging in context
		ctx := context.Background()
		var entries []run.ExecutedCommand
		ctx = run.LogCommands(ctx, func(e run.ExecutedCommand) {
			entries = append(entries, e)
			t.Logf("%+v", e)
		})

		// Run a command
		_ = run.Cmd(ctx, "echo 'hello world'").Run().Wait()

		// Check logged results
		c.Assert(entries, qt.HasLen, 1)
		c.Assert(entries[0].Args, qt.CmpEquals(), []string{"echo", "hello world"})
	})

	c.Run("Tracing", func(c *qt.C) {
		// Enable tracing in context
		ctx := context.Background()
		ctx = run.TraceCommands(ctx, run.DefaultTraceAttributes)

		c.Run("Wait", func(c *qt.C) {
			// Set up tracing mock
			traces := tracetest.NewSpanRecorder()
			otel.SetTracerProvider(trace.NewTracerProvider(
				trace.WithSpanProcessor(traces),
			))

			// Run a command
			_ = run.Cmd(ctx, "echo 'hello world'").Run().Wait()

			// Check created spans
			spans := traces.Ended()
			c.Assert(spans, qt.HasLen, 1)
			// The name has the full evaluated path of a command, which varies from
			// environment to environment, so we do a substring match.
			c.Assert(spans[0].Name(), qt.Contains, "Run")
			c.Assert(spans[0].Name(), qt.Contains, "/echo")
			c.Assert(spans[0].Events(), qt.HasLen, 2)     // Wait, Done
			c.Assert(spans[0].Attributes(), qt.HasLen, 2) // Args, Dir
		})

		c.Run("Stream (more complicated example)", func(c *qt.C) {
			// Set up tracing mock
			traces := tracetest.NewSpanRecorder()
			otel.SetTracerProvider(trace.NewTracerProvider(
				trace.WithSpanProcessor(traces),
			))

			// Run a command
			_ = run.Cmd(ctx, "echo 'hello world'").Run().Stream(io.Discard)

			// Span is ended asynchronously
			time.Sleep(1 * time.Millisecond)

			// Check created spans
			spans := traces.Ended()
			c.Assert(spans, qt.HasLen, 1)
			// The name has the full evaluated path of a command, which varies from
			// environment to environment, so we do a substring match.
			c.Assert(spans[0].Name(), qt.Contains, "Run")
			c.Assert(spans[0].Name(), qt.Contains, "/echo")
			c.Assert(spans[0].Events(), qt.HasLen, 3)     // Stream, WriteTo, Done
			c.Assert(spans[0].Attributes(), qt.HasLen, 2) // Args, Dir
		})
	})
}
