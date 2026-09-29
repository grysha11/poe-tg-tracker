package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/gatewayclient"
	"github.com/grysha11/poe-tg-tracker/internal/telegram"
)

func probe(t *testing.T, h *health, path string) int {
	t.Helper()
	mux := http.NewServeMux()
	h.register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

func TestHealthz_FailsWhenLoopStalls(t *testing.T) {
	h := &health{}
	h.beat()
	if code := probe(t, h, "/healthz"); code != http.StatusOK {
		t.Errorf("fresh heartbeat: /healthz = %d, want 200", code)
	}

	h.loopAt.Store(time.Now().Add(-livenessWindow - time.Second).UnixNano())
	if code := probe(t, h, "/healthz"); code != http.StatusServiceUnavailable {
		t.Errorf("stale heartbeat: /healthz = %d, want 503", code)
	}
}

func TestReadyz_NeedsRecentPollAndReadyGateway(t *testing.T) {
	gatewayReady := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !gatewayReady {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(ts.Close)

	h := &health{gw: gatewayclient.New(ts.URL)}
	if code := probe(t, h, "/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("before first poll: /readyz = %d, want 503", code)
	}

	h.pollOK()
	if code := probe(t, h, "/readyz"); code != http.StatusOK {
		t.Errorf("after poll, gateway ready: /readyz = %d, want 200", code)
	}

	gatewayReady = false
	if code := probe(t, h, "/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("gateway not ready: /readyz = %d, want 503", code)
	}
}

func TestPollBackoff(t *testing.T) {
	transient := errors.New("boom")
	for failures, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 6: 32 * time.Second, 7: maxPollBackoff, 50: maxPollBackoff} {
		if got := pollBackoff(failures, transient); got != want {
			t.Errorf("pollBackoff(%d) = %v, want %v", failures, got, want)
		}
	}

	flood := &telegram.APIError{Method: "getUpdates", Status: http.StatusTooManyRequests, RetryAfter: 90 * time.Second}
	if got := pollBackoff(1, flood); got != 90*time.Second {
		t.Errorf("pollBackoff with retry_after = %v, want 90s", got)
	}
}
