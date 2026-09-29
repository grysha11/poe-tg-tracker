package main

import (
	"context"
	"fmt"
	"os"
	"time"

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

	start := time.Now()
	log.Info("migrate started")
	results, version, err := db.Migrate(ctx, dbDSN)
	for _, r := range results {
		if r.Error != nil {
			log.Error("migration failed", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration, "err", r.Error)
			continue
		}
		log.Info("migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	if err != nil {
		log.Error("migrate failed", "err", err, "dur", time.Since(start))
		return 1
	}
	log.Info("migrate complete", "applied", len(results), "version", version, "dur", time.Since(start))
	return 0
}
