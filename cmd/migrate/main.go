package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/db"
	"github.com/grysha11/poe-tg-tracker/internal/logger"
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
		fmt.Fprintln(os.Stderr, "migrate: DB_DSN env required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	results, version, err := db.Migrate(ctx, dbDSN)
	for _, r := range results {
		log.Info("migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	if err != nil {
		log.Error("migrate failed", "err", err)
		os.Exit(1)
	}
	log.Info("migrate complete", "applied", len(results), "version", version)
}
