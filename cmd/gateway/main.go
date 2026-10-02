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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/grpcclient"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
	"github.com/grysha11/poe-tg-tracker/internal/telemetry"
)

func main() {
	os.Exit(run())
}

func run() int {
	config.LoadDotEnv()

	upstream := os.Getenv("EXCHANGE_SERVICE_ADDR")
	if upstream == "" {
		fmt.Fprintln(os.Stderr, "gateway: EXCHANGE_SERVICE_ADDR env required")
		return 1
	}

	addr := os.Getenv("HTTP_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tel, err := telemetry.Setup(ctx, "gateway")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway: telemetry setup failed: %v\n", err)
		return 1
	}
	defer tel.ShutdownWithTimeout()
	log := tel.Log

	telemetry.ServeMetrics(ctx, log, telemetry.NewMetricsMux())

	conn, err := grpcclient.Dial(upstream)
	if err != nil {
		log.Error("dial exchange-service failed", "addr", upstream, "err", err)
		return 1
	}
	defer conn.Close()

	handler, err := newHandler(ctx, conn, log)
	if err != nil {
		log.Error("build handler failed", "err", err)
		return 1
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
		return 1
	}
	return 0
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

	return otelhttp.NewHandler(logRequests(log, mux), "gateway",
		otelhttp.WithFilter(notProbe),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path }),
	), nil
}

func notProbe(r *http.Request) bool {
	return r.URL.Path != "/healthz" && r.URL.Path != "/readyz"
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

		attrs := []any{"method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery, "status", rec.status, "dur", time.Since(start)}
		ctx := r.Context()
		switch {
		case !notProbe(r):
			if rec.status >= 500 {
				log.WarnContext(ctx, "probe failed", attrs...)
			} else {
				log.DebugContext(ctx, "probe", attrs...)
			}
		case rec.status >= 500:
			log.ErrorContext(ctx, "request failed", attrs...)
		case rec.status >= 400:
			log.WarnContext(ctx, "request rejected", attrs...)
		default:
			log.InfoContext(ctx, "request", attrs...)
		}
	})
}
