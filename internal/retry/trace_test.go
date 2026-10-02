package retry_test

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/grysha11/poe-tg-tracker/internal/retry"
)

func TestDo_AddsRetryEventsToTheCurrentSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")

	calls := 0
	err := retry.Do(ctx, "test", fast, func(context.Context) error {
		calls++
		if calls < 3 {
			return retry.Retryable(errors.New("transient"))
		}
		return nil
	})
	span.End()
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	events := spans[0].Events
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (one per retry)", len(events))
	}
	for i, e := range events {
		if e.Name != "retry" {
			t.Errorf("event %d name = %q, want retry", i, e.Name)
		}
	}
}
