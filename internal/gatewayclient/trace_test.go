package gatewayclient_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestGet_PropagatesTraceContext(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetTracerProvider(sdktrace.NewTracerProvider())

	var traceparent string
	client := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
		w.Write([]byte(`{"leagues":[]}`))
	})

	ctx, span := otel.Tracer("test").Start(context.Background(), "bot.message")
	defer span.End()
	if _, err := client.ListLeagues(ctx); err != nil {
		t.Fatalf("ListLeagues: %v", err)
	}

	traceID := span.SpanContext().TraceID().String()
	if !strings.Contains(traceparent, traceID) {
		t.Errorf("traceparent = %q, want it to carry trace id %s", traceparent, traceID)
	}
}
