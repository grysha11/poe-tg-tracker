package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

const namespace = "poetracker"

type Telemetry struct {
	Log       *slog.Logger
	shutdowns []func(context.Context) error
}

type Option func(*options)

type options struct {
	logWriter io.Writer
}

func WithLogWriter(w io.Writer) Option {
	return func(o *options) { o.logWriter = w }
}

func Setup(ctx context.Context, service string, opts ...Option) (*Telemetry, error) {
	o := options{logWriter: os.Stdout}
	for _, opt := range opts {
		opt(&o)
	}

	version := Version()
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", service),
			attribute.String("service.namespace", namespace),
			attribute.String("service.version", version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	t := &Telemetry{}

	promExporter, err := otelprom.New()
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(promExporter))
	otel.SetMeterProvider(mp)
	t.shutdowns = append(t.shutdowns, mp.Shutdown)

	tp, err := newTracerProvider(ctx, res)
	if err != nil {
		return nil, err
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.shutdowns = append(t.shutdowns, tp.Shutdown)

	level := parseLevel(os.Getenv("LOG_LEVEL"))
	stdout := &traceHandler{Handler: slog.NewJSONHandler(o.logWriter, &slog.HandlerOptions{Level: level}).
		WithAttrs([]slog.Attr{slog.String("service", service), slog.String("service_version", version)})}

	handler := slog.Handler(stdout)
	if otlpEnabled("LOGS") {
		exporter, err := otlploggrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("otlp log exporter: %w", err)
		}
		lp := sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
		t.shutdowns = append(t.shutdowns, lp.Shutdown)

		otlp := &levelHandler{Handler: otelslog.NewHandler(namespace+"/"+service, otelslog.WithLoggerProvider(lp)), level: level}
		handler = slog.NewMultiHandler(stdout, otlp)
	}

	t.Log = slog.New(handler)
	slog.SetDefault(t.Log)
	return t, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	for _, shutdown := range slices.Backward(t.shutdowns) {
		errs = append(errs, shutdown(ctx))
	}
	return errors.Join(errs...)
}

func (t *Telemetry) ShutdownWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := t.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "telemetry shutdown: %v\n", err)
	}
}

func NewMetricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	return mux
}

func ServeMetrics(ctx context.Context, log *slog.Logger, handler http.Handler) {
	addr := os.Getenv("METRICS_LISTEN_ADDR")
	if addr == "" {
		addr = ":9464"
	}
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	go func() {
		log.Info("metrics listener starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics listener failed", "addr", addr, "err", err)
		}
	}()
}

func Version() string {
	if v := os.Getenv("VERSION"); v != "" {
		return v
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return "dev"
}

func otlpEnabled(signal string) bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_"+signal+"_ENDPOINT") != ""
}

func parseLevel(s string) slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return level
}

type levelHandler struct {
	slog.Handler
	level slog.Leveler
}

func (h *levelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level.Level() && h.Handler.Enabled(ctx, l)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{Handler: h.Handler.WithAttrs(attrs), level: h.level}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{Handler: h.Handler.WithGroup(name), level: h.level}
}
