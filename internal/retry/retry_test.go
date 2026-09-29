package retry_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/retry"
)

var fast = retry.Policy{Attempts: 3, Base: time.Millisecond, Max: 5 * time.Millisecond}

func TestDo_RetriesUntilSuccess(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), "test", fast, func(context.Context) error {
		calls++
		if calls < 3 {
			return retry.Retryable(errors.New("transient"))
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err = %v, calls = %d, want nil after 3 calls", err, calls)
	}
}

func TestDo_StopsOnNonRetryable(t *testing.T) {
	calls := 0
	want := errors.New("permanent")
	err := retry.Do(context.Background(), "test", fast, func(context.Context) error {
		calls++
		return want
	})
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("err = %v, calls = %d, want permanent after 1 call", err, calls)
	}
}

func TestDo_ReturnsUnwrappedErrorWhenExhausted(t *testing.T) {
	calls := 0
	want := errors.New("still down")
	err := retry.Do(context.Background(), "test", fast, func(context.Context) error {
		calls++
		return retry.Retryable(want)
	})
	if err != want || calls != fast.Attempts {
		t.Fatalf("err = %#v, calls = %d, want the original error after %d calls", err, calls, fast.Attempts)
	}
}

func TestDo_HonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	slow := retry.Policy{Attempts: 5, Base: time.Hour, Max: time.Hour}

	calls := 0
	err := retry.Do(ctx, "test", slow, func(context.Context) error {
		calls++
		cancel()
		return retry.Retryable(errors.New("transient"))
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err = %v, calls = %d, want context.Canceled after 1 call", err, calls)
	}
}

func TestDo_GivesUpWhenRetryAfterExceedsDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	calls := 0
	start := time.Now()
	err := retry.Do(ctx, "test", fast, func(context.Context) error {
		calls++
		return retry.RetryableAfter(errors.New("rate limited"), time.Minute)
	})
	if err == nil || calls != 1 || time.Since(start) > 40*time.Millisecond {
		t.Fatalf("err = %v, calls = %d, took %v, want immediate give-up", err, calls, time.Since(start))
	}
}

func TestAfterHeader(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 0, "3": 3 * time.Second, "-1": 0, "soon": 0} {
		h := http.Header{}
		if in != "" {
			h.Set("Retry-After", in)
		}
		if got := retry.AfterHeader(h); got != want {
			t.Errorf("AfterHeader(%q) = %v, want %v", in, got, want)
		}
	}
}
