package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/db"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
	"github.com/grysha11/poe-tg-tracker/internal/exchangesvc"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
	"github.com/grysha11/poe-tg-tracker/internal/poe2scout"
	"github.com/grysha11/poe-tg-tracker/internal/retry"
	"github.com/grysha11/poe-tg-tracker/internal/telemetry"
)

const dbWatchInterval = 10 * time.Second

func main() {
	os.Exit(run())
}

func run() int {
	config.LoadDotEnv()

	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		fmt.Fprintln(os.Stderr, "exchange-service: DB_DSN env required")
		return 1
	}

	contact := os.Getenv("POE_CONTACT")
	if contact == "" {
		fmt.Fprintln(os.Stderr, "exchange-service: POE_CONTACT env required")
		return 1
	}
	userAgent := fmt.Sprintf("poe-tg-tracker-exchange-service/0.1.0 (contact: %s)", contact)

	league := os.Getenv("POE_LEAGUE")
	if league == "" {
		fmt.Fprintln(os.Stderr, "exchange-service: POE_LEAGUE env required")
		return 1
	}

	addr := os.Getenv("GRPC_LISTEN_ADDR")
	if addr == "" {
		addr = ":9090"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tel, err := telemetry.Setup(ctx, "exchange-service")
	if err != nil {
		fmt.Fprintf(os.Stderr, "exchange-service: telemetry setup failed: %v\n", err)
		return 1
	}
	defer tel.ShutdownWithTimeout()
	log := tel.Log

	telemetry.ServeMetrics(ctx, log, telemetry.NewMetricsMux())

	dbase, err := db.Open(dbDSN)
	if err != nil {
		log.Error("db open failed", "err", err)
		return 1
	}
	defer dbase.Close()

	if _, err := exchangesvc.RegisterDBGauges(dbase.Q, league); err != nil {
		log.Warn("register db gauges failed", "err", err)
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("listen failed", "addr", addr, "err", err)
		return 1
	}

	srv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithFilter(filters.Not(filters.HealthCheck())))),
		grpc.UnaryInterceptor(logUnary(log)),
	)

	digests := exchange.NewClient(userAgent)
	digests.Retry = retry.Policy{Attempts: 3, Base: 500 * time.Millisecond, Max: 4 * time.Second}

	pb.RegisterExchangeQueryServiceServer(srv, &exchangesvc.QueryServer{
		Q:             dbase.Q,
		Digests:       digests,
		DefaultLeague: league,
	})
	pb.RegisterExchangeAdminServiceServer(srv, &exchangesvc.AdminServer{
		Q:             dbase.Q,
		Scout:         poe2scout.NewClient(userAgent),
		DefaultLeague: league,
	})

	healthSrv := exchangesvc.NewHealthServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	reflection.Register(srv)

	go exchangesvc.WatchDB(ctx, log, dbase, healthSrv, dbWatchInterval)

	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		healthSrv.Shutdown()
		srv.GracefulStop()
	}()

	log.Info("starting", "addr", addr, "league", league)
	if err := srv.Serve(lis); err != nil {
		log.Error("serve failed", "err", err)
		return 1
	}
	return 0
}

func logUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		code := status.Code(err)
		attrs := []any{"method", info.FullMethod, "code", code.String(), "dur", time.Since(start)}

		switch {
		case strings.HasPrefix(info.FullMethod, "/grpc.health.v1.Health/"):
			log.DebugContext(ctx, "health check", attrs...)
		case err == nil:
			log.InfoContext(ctx, "rpc ok", attrs...)
		case code == codes.InvalidArgument || code == codes.NotFound || code == codes.FailedPrecondition || code == codes.Canceled:
			log.WarnContext(ctx, "rpc rejected", append(attrs, "err", err)...)
		default:
			log.ErrorContext(ctx, "rpc failed", append(attrs, "err", err)...)
		}
		return resp, err
	}
}
