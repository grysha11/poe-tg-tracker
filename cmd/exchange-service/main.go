package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/db"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
	"github.com/grysha11/poe-tg-tracker/internal/exchangesvc"
	"github.com/grysha11/poe-tg-tracker/internal/logger"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

func main() {
	config.LoadDotEnv()

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	log := logger.New(logLevel)

	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		fmt.Fprintln(os.Stderr, "exchange-service: DB_DSN env required")
		os.Exit(1)
	}

	contact := os.Getenv("POE_CONTACT")
	if contact == "" {
		fmt.Fprintln(os.Stderr, "exchange-service: POE_CONTACT env required")
		os.Exit(1)
	}
	userAgent := fmt.Sprintf("poe-tg-tracker-exchange-service/0.1.0 (contact: %s)", contact)

	league := os.Getenv("POE_LEAGUE")
	if league == "" {
		league = "Forbidden Rites"
	}

	addr := os.Getenv("GRPC_LISTEN_ADDR")
	if addr == "" {
		addr = ":9090"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbase, err := db.Open(dbDSN)
	if err != nil {
		log.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer dbase.Close()

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("listen failed", "addr", addr, "err", err)
		os.Exit(1)
	}

	srv := grpc.NewServer(grpc.UnaryInterceptor(logUnary(log)))

	pb.RegisterExchangeQueryServiceServer(srv, &exchangesvc.QueryServer{
		Q:             dbase.Q,
		Client:        exchange.NewClient(userAgent),
		DefaultLeague: league,
	})

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	reflection.Register(srv)

	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		healthSrv.Shutdown()
		srv.GracefulStop()
	}()

	log.Info("starting", "addr", addr, "league", league)
	if err := srv.Serve(lis); err != nil {
		log.Error("serve failed", "err", err)
		os.Exit(1)
	}
}

func logUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		if err != nil {
			log.Error("rpc failed", "method", info.FullMethod, "code", status.Code(err).String(), "err", err, "dur", time.Since(start))
		} else {
			log.Debug("rpc ok", "method", info.FullMethod, "dur", time.Since(start))
		}
		return resp, err
	}
}
