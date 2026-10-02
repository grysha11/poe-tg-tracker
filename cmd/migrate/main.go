package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/db"
	"github.com/grysha11/poe-tg-tracker/internal/telemetry"
)

func main() {
	os.Exit(run())
}

func run() int {
	config.LoadDotEnv()

	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		fmt.Fprintln(os.Stderr, "migrate: DB_DSN env required")
		return 1
	}

	tel, err := telemetry.Setup(context.Background(), "migrate")
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: telemetry setup failed: %v\n", err)
		return 1
	}
	defer tel.ShutdownWithTimeout()
	log := tel.Log

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	ctx, span := otel.Tracer("poetracker/migrate").Start(ctx, "migrate")

	start := time.Now()
	log.InfoContext(ctx, "migrate started")
	results, version, err := db.Migrate(ctx, dbDSN)
	span.SetAttributes(attribute.Int("migrate.applied", len(results)), attribute.Int64("migrate.version", version))
	telemetry.EndSpan(span, err)

	for _, r := range results {
		if r.Error != nil {
			log.ErrorContext(ctx, "migration failed", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration, "err", r.Error)
			continue
		}
		log.InfoContext(ctx, "migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	if err != nil {
		log.ErrorContext(ctx, "migrate failed", "err", err, "dur", time.Since(start))
		return 1
	}
	log.InfoContext(ctx, "migrate complete", "applied", len(results), "version", version, "dur", time.Since(start))
	return 0
}
