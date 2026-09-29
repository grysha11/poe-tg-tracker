package telemetry

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func HTTPClient(name string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: otelhttp.NewTransport(&loggingTransport{name: name, next: http.DefaultTransport}),
	}
}

type loggingTransport struct {
	name string
	next http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	attrs := []any{"client", t.name, "method", req.Method, "host", req.URL.Host, "path", req.URL.Path, "dur", time.Since(start)}
	if err != nil {
		slog.DebugContext(req.Context(), "http request failed", append(attrs, "err", err)...)
		return nil, err
	}
	slog.DebugContext(req.Context(), "http request", append(attrs, "status", resp.StatusCode, "bytes", resp.ContentLength)...)
	return resp, nil
}
