package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTraceHandler_AddsIDsOnlyInsideASpan(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(&traceHandler{Handler: slog.NewJSONHandler(&buf, nil)}).With("service", "test")

	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	log.InfoContext(ctx, "inside")
	span.End()
	log.InfoContext(context.Background(), "outside")

	var inside, outside map[string]any
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2", len(lines))
	}
	if err := json.Unmarshal(lines[0], &inside); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[1], &outside); err != nil {
		t.Fatal(err)
	}

	sc := span.SpanContext()
	if inside["trace_id"] != sc.TraceID().String() || inside["span_id"] != sc.SpanID().String() {
		t.Errorf("inside = %v, want trace_id %s span_id %s", inside, sc.TraceID(), sc.SpanID())
	}
	if inside["service"] != "test" {
		t.Errorf("inside lost With attrs: %v", inside)
	}
	if _, ok := outside["trace_id"]; ok {
		t.Errorf("outside a span the line has a trace_id: %v", outside)
	}
}

func TestEndSpan_RecordsError(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	_, ok := tp.Tracer("test").Start(context.Background(), "ok")
	EndSpan(ok, nil)
	_, failed := tp.Tracer("test").Start(context.Background(), "failed")
	EndSpan(failed, errors.New("boom"))

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2", len(spans))
	}
	if spans[0].Status.Code != codes.Unset {
		t.Errorf("ok span status = %v, want Unset", spans[0].Status.Code)
	}
	if spans[1].Status.Code != codes.Error || spans[1].Status.Description != "boom" || len(spans[1].Events) != 1 {
		t.Errorf("failed span = status %v %q, %d events; want Error boom with 1 exception event",
			spans[1].Status.Code, spans[1].Status.Description, len(spans[1].Events))
	}
}

func TestHasSpan(t *testing.T) {
	if HasSpan(context.Background()) {
		t.Error("HasSpan(background) = true")
	}
	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "op")
	defer span.End()
	if !HasSpan(ctx) {
		t.Error("HasSpan inside a span = false")
	}
}
