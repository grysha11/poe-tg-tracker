package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/db"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
	"github.com/grysha11/poe-tg-tracker/internal/ingest"
	"github.com/grysha11/poe-tg-tracker/internal/logger"
)

var backoff = []time.Duration{0, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second}

func main() {
	hourFlag := flag.Int64("hour", 0, "unix timestamp of hour to fetch (default: last settled hour)")
	outFlag := flag.String("out", "", "output file path (default: $FETCH_OUT_PATH or /tmp/poe-fetch/last_fetch.json)")
	flag.Parse()

	config.LoadDotEnv()

	contact := os.Getenv("POE_CONTACT")
	if contact == "" {
		fmt.Fprintln(os.Stderr, "fetcher: POE_CONTACT env required")
		os.Exit(1)
	}
	userAgent := fmt.Sprintf("poe-tg-tracker-fetcher/0.1.0 (contact: %s)", contact)

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	log := logger.New(logLevel)

	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		fmt.Fprintln(os.Stderr, "fetcher: DB_DSN env required")
		os.Exit(1)
	}

	hour := exchange.AlignHour(time.Now()).Add(-time.Hour)
	if *hourFlag != 0 {
		hour = exchange.AlignHour(time.Unix(*hourFlag, 0))
	}

	outPath := *outFlag
	if outPath == "" {
		outPath = os.Getenv("FETCH_OUT_PATH")
	}
	if outPath == "" {
		outPath = "/tmp/poe-fetch/last_fetch.json"
	}

	client := exchange.NewClient(userAgent)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prev, _ := os.ReadFile(outPath)

	var (
		raw []byte
		err error
	)
	for attempt, wait := range backoff {
		if wait > 0 {
			time.Sleep(wait)
		}
		raw, err = client.FetchRaw(ctx, hour.Unix())
		if err != nil {
			log.Warn("fetch attempt failed", "attempt", attempt+1, "err", err)
			continue
		}
		if len(prev) > 0 && bytes.Equal(prev, raw) {
			err = fmt.Errorf("unchanged data since last fetch")
			log.Warn("fetch attempt returned unchanged data, retrying", "attempt", attempt+1)
			continue
		}
		break
	}
	if err != nil {
		log.Error("fetch failed, giving up", "hour", hour.Format(time.RFC3339), "err", err)
		os.Exit(1)
	}

	var digest exchange.Digest
	if err := json.Unmarshal(raw, &digest); err != nil {
		log.Error("decode digest failed", "err", err)
		os.Exit(1)
	}

	dbase, err := db.Open(dbDSN)
	if err != nil {
		log.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer dbase.Close()

	fetchedAt := time.Now()
	stats, err := ingest.Run(ctx, dbase, log, &digest, hour, fetchedAt)
	if err != nil {
		log.Error("ingest failed", "hour", hour.Format(time.RFC3339), "err", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		log.Error("mkdir failed", "path", outPath, "err", err)
		os.Exit(1)
	}

	tmp := outPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		log.Error("write failed", "path", tmp, "err", err)
		os.Exit(1)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		log.Error("rename failed", "path", outPath, "err", err)
		os.Exit(1)
	}

	log.Info("fetch complete",
		"hour", hour.Format(time.RFC3339),
		"bytes", len(raw),
		"out", outPath,
		"markets_seen", stats.MarketsSeen,
		"markets_inserted", stats.MarketsInserted,
		"markets_skipped", stats.MarketsSkipped,
		"new_currencies", len(stats.NewCurrencies),
	)
}
