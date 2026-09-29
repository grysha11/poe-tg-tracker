package retry

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Policy struct {
	Attempts int
	Base     time.Duration
	Max      time.Duration
}

type retryableError struct {
	err   error
	after time.Duration
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return &retryableError{err: err}
}

func RetryableAfter(err error, after time.Duration) error {
	if err == nil {
		return nil
	}
	return &retryableError{err: err, after: after}
}

var retries metric.Int64Counter

func init() {
	var err error
	retries, err = otel.Meter("poetracker/retry").Int64Counter("poetracker.retries",
		metric.WithDescription("Retry events by client: retry = one extra attempt, recovered/exhausted = final outcome after retrying."))
	if err != nil {
		otel.Handle(err)
	}
}

func Do(ctx context.Context, client string, p Policy, fn func(context.Context) error) error {
	for attempt := 1; ; attempt++ {
		err := fn(ctx)

		var re *retryableError
		if !errors.As(err, &re) {
			if attempt > 1 {
				record(ctx, client, outcome(err))
			}
			return err
		}
		if attempt >= p.Attempts {
			record(ctx, client, "exhausted")
			return re.err
		}

		wait := max(p.delay(attempt), re.after)
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			record(ctx, client, "exhausted")
			return re.err
		}

		record(ctx, client, "retry")
		slog.WarnContext(ctx, "retrying", "client", client, "attempt", attempt, "max_attempts", p.Attempts, "wait", wait, "err", re.err)

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(re.err, ctx.Err())
		case <-timer.C:
		}
	}
}

func (p Policy) delay(attempt int) time.Duration {
	d := p.Base << (attempt - 1)
	if d <= 0 || d > p.Max {
		d = p.Max
	}
	if d <= 0 {
		return 0
	}
	return rand.N(d + 1)
}

func AfterHeader(h http.Header) time.Duration {
	secs, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

func outcome(err error) string {
	if err == nil {
		return "recovered"
	}
	return "exhausted"
}

func record(ctx context.Context, client, outcome string) {
	retries.Add(ctx, 1, metric.WithAttributes(attribute.String("client", client), attribute.String("outcome", outcome)))
}
