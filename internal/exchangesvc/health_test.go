package exchangesvc_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grysha11/poe-tg-tracker/internal/exchangesvc"
)

type fakePinger struct{ down atomic.Bool }

func (f *fakePinger) PingContext(context.Context) error {
	if f.down.Load() {
		return errors.New("connection refused")
	}
	return nil
}

func statusOf(t *testing.T, hs *health.Server, service string) healthpb.HealthCheckResponse_ServingStatus {
	t.Helper()
	resp, err := hs.Check(context.Background(), &healthpb.HealthCheckRequest{Service: service})
	if err != nil {
		t.Fatalf("health check %q: %v", service, err)
	}
	return resp.GetStatus()
}

func waitFor(t *testing.T, hs *health.Server, want healthpb.HealthCheckResponse_ServingStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if statusOf(t, hs, "") == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("overall status never became %v", want)
}

func TestWatchDB_ReadinessFollowsDBAndLivenessStaysUp(t *testing.T) {
	hs := exchangesvc.NewHealthServer()
	if got := statusOf(t, hs, ""); got != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("initial status = %v, want NOT_SERVING before the first DB ping", got)
	}

	db := &fakePinger{}
	db.down.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go exchangesvc.WatchDB(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), db, hs, 5*time.Millisecond)

	time.Sleep(30 * time.Millisecond)
	if got := statusOf(t, hs, ""); got != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status with DB down = %v, want NOT_SERVING", got)
	}

	db.down.Store(false)
	waitFor(t, hs, healthpb.HealthCheckResponse_SERVING)

	db.down.Store(true)
	waitFor(t, hs, healthpb.HealthCheckResponse_NOT_SERVING)

	if got := statusOf(t, hs, exchangesvc.LivenessService); got != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("liveness = %v with DB down, want SERVING", got)
	}
}
