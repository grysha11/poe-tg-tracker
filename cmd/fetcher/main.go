package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/db"
	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
	"github.com/grysha11/poe-tg-tracker/internal/ingest"
	"github.com/grysha11/poe-tg-tracker/internal/logger"
)

var backoff = []time.Duration{0, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second}

func main() {
	hourFlag := flag.Int64("hour", 0, "unix timestamp of hour to fetch (default: last settled hour)")
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

	client := exchange.NewClient(userAgent)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dbase, err := db.Open(dbDSN)
	if err != nil {
		log.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer dbase.Close()

	var prevHash string
	switch prev, err := dbase.Q.LatestFetchBefore(ctx, hour.Unix()); {
	case err == nil:
		prevHash = prev.PayloadSha256
	case !errors.Is(err, sql.ErrNoRows):
		log.Error("load previous fetch failed", "err", err)
		os.Exit(1)
	}

	var (
		raw []byte
		sum string
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
		hashed := sha256.Sum256(raw)
		sum = hex.EncodeToString(hashed[:])
		if prevHash != "" && sum == prevHash {
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

	fetchedAt := time.Now()
	stats, err := ingest.Run(ctx, dbase, log, &digest, hour, fetchedAt)
	if err != nil {
		log.Error("ingest failed", "hour", hour.Format(time.RFC3339), "err", err)
		os.Exit(1)
	}

	if err := dbase.Q.RecordFetch(ctx, dbgen.RecordFetchParams{
		HourUtc:       hour.Unix(),
		PayloadSha256: sum,
		FetchedAt:     fetchedAt.Unix(),
	}); err != nil {
		log.Error("record fetch failed", "err", err)
		os.Exit(1)
	}

	log.Info("fetch complete",
		"hour", hour.Format(time.RFC3339),
		"bytes", len(raw),
		"markets_seen", stats.MarketsSeen,
		"markets_inserted", stats.MarketsInserted,
		"markets_skipped", stats.MarketsSkipped,
		"new_currencies", len(stats.NewCurrencies),
	)
}
