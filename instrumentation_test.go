package run_test

import (
	"context"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/sourcegraph/run"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestInstrumentation(t *testing.T) {
	c := qt.New(t)

	c.Run("Logging", func(c *qt.C) {
		// Enable logging in context
		ctx := context.Background()
		var entries []run.ExecutedCommand
		ctx = run.LogCommands(ctx, func(e run.ExecutedCommand) {
			entries = append(entries, e)
		})

		// Run a command
		_ = run.Cmd(ctx, "foobar").Run().Wait()

		// Check logged results
		c.Assert(entries, qt.HasLen, 1)
		c.Assert(entries[0].Args, qt.CmpEquals(), []string{"foobar"})
	})

	c.Run("Tracing", func(c *qt.C) {
		// Enable tracing in context
		ctx := context.Background()
		var traces mockTracerProvider
		otel.SetTracerProvider(&traces)
		ctx = run.TraceCommands(ctx, run.DefaultTraceAttributes)

		// Run a command
		_ = run.Cmd(ctx, "foobar").Run().Wait()

		c.Assert(traces.spans, qt.HasLen, 1)
		c.Assert(traces.spans, qt.CmpEquals(), []string{"Run foobar"})
	})

}

type mockTracerProvider struct {
	spans []string
}

func (m *mockTracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return &mockTracer{provider: m}
}

type mockTracer struct {
	provider *mockTracerProvider
}

func (m *mockTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	m.provider.spans = append(m.provider.spans, spanName)
	return trace.NewNoopTracerProvider().Tracer("").Start(ctx, spanName, opts...)
}
