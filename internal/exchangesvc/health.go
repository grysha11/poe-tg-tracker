package exchangesvc

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const LivenessService = "liveness"

type Pinger interface {
	PingContext(ctx context.Context) error
}

func NewHealthServer() *health.Server {
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	hs.SetServingStatus(LivenessService, healthpb.HealthCheckResponse_SERVING)
	return hs
}

func WatchDB(ctx context.Context, log *slog.Logger, db Pinger, hs *health.Server, interval time.Duration) {
	serving := false
	check := func(first bool) {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := db.PingContext(pingCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}

		switch {
		case err == nil && !serving:
			serving = true
			hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
			log.InfoContext(ctx, "db reachable, serving")
		case err != nil && (serving || first):
			serving = false
			hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
			log.ErrorContext(ctx, "db unreachable, not serving", "err", err)
		case err != nil:
			log.WarnContext(ctx, "db still unreachable", "err", err)
		}
	}

	check(true)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check(false)
		}
	}
}
