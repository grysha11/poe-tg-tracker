package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/grysha11/poe-tg-tracker/internal/config"
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

	upstream := os.Getenv("EXCHANGE_SERVICE_ADDR")
	if upstream == "" {
		fmt.Fprintln(os.Stderr, "gateway: EXCHANGE_SERVICE_ADDR env required")
		os.Exit(1)
	}

	addr := os.Getenv("HTTP_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := grpc.NewClient(upstream, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("dial exchange-service failed", "addr", upstream, "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	handler, err := newHandler(ctx, conn, log)
	if err != nil {
		log.Error("build handler failed", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Info("starting", "addr", addr, "upstream", upstream)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve failed", "err", err)
		os.Exit(1)
	}
}

func newHandler(ctx context.Context, conn *grpc.ClientConn, log *slog.Logger) (http.Handler, error) {
	gw := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
		MarshalOptions: protojson.MarshalOptions{EmitUnpopulated: true},
	}))
	if err := pb.RegisterExchangeQueryServiceHandler(ctx, gw, conn); err != nil {
		return nil, fmt.Errorf("register exchange query handler: %w", err)
	}

	health := healthpb.NewHealthClient(conn)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		resp, err := health.Check(checkCtx, &healthpb.HealthCheckRequest{})
		if err != nil || resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
			http.Error(w, "exchange-service not serving", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/v1/", gw)

	return logRequests(log, mux), nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", rec.status, "dur", time.Since(start)}
		switch {
		case rec.status >= 500:
			log.Error("request failed", attrs...)
		case r.URL.Path == "/healthz" || r.URL.Path == "/readyz":
			log.Debug("probe", attrs...)
		default:
			log.Info("request", attrs...)
		}
	})
}
