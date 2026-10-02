package telegram

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

var (
	spanExporter = tracetest.NewInMemoryExporter()
	installOnce  sync.Once
)

func recordSpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	installOnce.Do(func() {
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanExporter)))
	})
	spanExporter.Reset()
	return spanExporter
}

func spanNamed(spans tracetest.SpanStubs, name string) (tracetest.SpanStub, bool) {
	for _, s := range spans {
		if s.Name == name {
			return s, true
		}
	}
	return tracetest.SpanStub{}, false
}

func TestDo_SpanInsideATraceHasNoToken(t *testing.T) {
	exporter := recordSpans(t)
	b, _ := newTestBot(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true,"result":{}}`))
	})

	ctx, parent := otel.Tracer("test").Start(context.Background(), "bot.message")
	if err := b.SendMessage(ctx, 1, "hi", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	parent.End()

	span, ok := spanNamed(exporter.GetSpans(), "telegram sendMessage")
	if !ok {
		t.Fatalf("no \"telegram sendMessage\" span, got %d spans", len(exporter.GetSpans()))
	}
	if span.Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Errorf("span parent = %s, want the bot.message span", span.Parent.SpanID())
	}
	if span.SpanKind != trace.SpanKindClient {
		t.Errorf("span kind = %v, want client", span.SpanKind)
	}
	if dump := fmt.Sprintf("%+v", span); strings.Contains(dump, testToken) || strings.Contains(dump, "SECRET") {
		t.Errorf("span leaks the bot token: %s", dump)
	}
}

func TestDo_FailedCallMarksSpanWithoutToken(t *testing.T) {
	exporter := recordSpans(t)
	b, ts := newTestBot(t, func(http.ResponseWriter, *http.Request) {})
	ts.Close()

	ctx, parent := otel.Tracer("test").Start(context.Background(), "bot.message")
	if _, err := b.do(ctx, "answerCallbackQuery", map[string]any{}); err == nil {
		t.Fatal("do succeeded against a closed server")
	}
	parent.End()

	span, ok := spanNamed(exporter.GetSpans(), "telegram answerCallbackQuery")
	if !ok {
		t.Fatal("no span for the failed call")
	}
	if span.Status.Code != codes.Error {
		t.Errorf("span status = %v, want Error", span.Status.Code)
	}
	if dump := fmt.Sprintf("%+v", span); strings.Contains(dump, testToken) || strings.Contains(dump, "SECRET") {
		t.Errorf("failed span leaks the bot token: %s", dump)
	}
}

func TestDo_NoSpanWithoutAParent(t *testing.T) {
	exporter := recordSpans(t)
	b, _ := newTestBot(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true,"result":[]}`))
	})

	if _, err := b.GetUpdates(context.Background(), 0, 0); err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if spans := exporter.GetSpans(); len(spans) != 0 {
		t.Errorf("got %d spans for a parentless getUpdates, want 0", len(spans))
	}
}
