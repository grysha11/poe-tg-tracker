package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "123456:SECRET-token-value"

func newTestBot(t *testing.T, handler http.HandlerFunc) (*Bot, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	b := NewBot(testToken)
	b.base = ts.URL
	return b, ts
}

func TestTransportErrorDoesNotLeakToken(t *testing.T) {
	b, ts := newTestBot(t, func(http.ResponseWriter, *http.Request) {})
	ts.Close()

	_, err := b.GetUpdates(context.Background(), 0, 0)
	if err == nil {
		t.Fatal("GetUpdates succeeded against a closed server")
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error leaks the bot token: %v", err)
	}
	if !strings.Contains(err.Error(), "getUpdates") {
		t.Errorf("error = %v, want the API method name", err)
	}
}

func TestSendRetriesOn429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	b, _ := newTestBot(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`))
			return
		}
		w.Write([]byte(`{"ok":true,"result":{}}`))
	})

	start := time.Now()
	if err := b.SendMessage(context.Background(), 1, "hi", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
	if time.Since(start) < time.Second {
		t.Errorf("retried after %v, want at least retry_after=1s", time.Since(start))
	}
}

func TestSendDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	b, _ := newTestBot(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	})

	err := b.SendMessage(context.Background(), 1, "hi", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want *APIError 400", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestEditMessageTextIgnoresNotModified(t *testing.T) {
	b, _ := newTestBot(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`))
	})

	if err := b.EditMessageText(context.Background(), 1, 2, "same", nil); err != nil {
		t.Fatalf("EditMessageText: %v, want nil for not-modified", err)
	}
}

func TestGetUpdatesDecodesResult(t *testing.T) {
	b, _ := newTestBot(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"message":{"message_id":1,"chat":{"id":5},"text":"/rates"}}]}`))
	})

	updates, err := b.GetUpdates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 7 || updates[0].Message.Text != "/rates" {
		t.Errorf("updates = %+v", updates)
	}
}
