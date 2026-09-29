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
	"github.com/grysha11/poe-tg-tracker/internal/telemetry"
)

var backoff = []time.Duration{0, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second}

func main() {
	os.Exit(run())
}

func run() int {
	hourFlag := flag.Int64("hour", 0, "unix timestamp of hour to fetch (default: last settled hour)")
	flag.Parse()

	config.LoadDotEnv()

	contact := os.Getenv("POE_CONTACT")
	if contact == "" {
		fmt.Fprintln(os.Stderr, "fetcher: POE_CONTACT env required")
		return 1
	}
	userAgent := fmt.Sprintf("poe-tg-tracker-fetcher/0.1.0 (contact: %s)", contact)

	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		fmt.Fprintln(os.Stderr, "fetcher: DB_DSN env required")
		return 1
	}

	tel, err := telemetry.Setup(context.Background(), "fetcher")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetcher: telemetry setup failed: %v\n", err)
		return 1
	}
	defer tel.ShutdownWithTimeout()
	log := tel.Log

	hour := exchange.AlignHour(time.Now()).Add(-time.Hour)
	if *hourFlag != 0 {
		hour = exchange.AlignHour(time.Unix(*hourFlag, 0))
	}
	hourStr := hour.Format(time.RFC3339)
	log = log.With("hour", hourStr)
	log.Info("fetch started", "backfill", *hourFlag != 0)

	runStart := time.Now()
	client := exchange.NewClient(userAgent)

	dbase, err := db.Open(dbDSN)
	if err != nil {
		log.Error("db open failed", "err", err)
		return 1
	}
	defer dbase.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var prevHash string
	switch prev, err := dbase.Q.LatestFetchBefore(ctx, hour.Unix()); {
	case err == nil:
		prevHash = prev.PayloadSha256
		log.Debug("previous fetch loaded", "prev_hour", time.Unix(prev.HourUtc, 0).UTC().Format(time.RFC3339), "prev_sha256", prevHash)
	case errors.Is(err, sql.ErrNoRows):
		log.Info("no previous fetch, skipping stale-payload check")
	default:
		log.Error("load previous fetch failed", "err", err)
		return 1
	}

	var (
		raw []byte
		sum string
	)
	fetchStart := time.Now()
	for attempt, wait := range backoff {
		if wait > 0 {
			select {
			case <-ctx.Done():
				log.Error("fetch deadline exceeded while waiting to retry", "attempt", attempt+1, "err", ctx.Err())
				return 1
			case <-time.After(wait):
			}
		}
		attemptStart := time.Now()
		raw, err = client.FetchRaw(ctx, hour.Unix())
		if err != nil {
			log.Warn("fetch attempt failed", "attempt", attempt+1, "max_attempts", len(backoff), "dur", time.Since(attemptStart), "err", err)
			continue
		}
		hashed := sha256.Sum256(raw)
		sum = hex.EncodeToString(hashed[:])
		if prevHash != "" && sum == prevHash {
			err = fmt.Errorf("unchanged data since last fetch")
			log.Warn("fetch attempt returned unchanged data, retrying", "attempt", attempt+1, "max_attempts", len(backoff), "sha256", sum)
			continue
		}
		log.Info("fetch attempt succeeded", "attempt", attempt+1, "bytes", len(raw), "sha256", sum, "dur", time.Since(attemptStart))
		break
	}
	if err != nil {
		log.Error("fetch failed, giving up", "attempts", len(backoff), "dur", time.Since(fetchStart), "err", err)
		return 1
	}
	fetchDur := time.Since(fetchStart)

	decodeStart := time.Now()
	var digest exchange.Digest
	if err := json.Unmarshal(raw, &digest); err != nil {
		log.Error("decode digest failed", "bytes", len(raw), "err", err)
		return 1
	}
	decodeDur := time.Since(decodeStart)

	ingestStart := time.Now()
	fetchedAt := time.Now()
	stats, err := ingest.Run(ctx, dbase, log, &digest, hour, fetchedAt)
	if err != nil {
		log.Error("ingest failed", "markets_seen", stats.MarketsSeen, "markets_inserted", stats.MarketsInserted, "dur", time.Since(ingestStart), "err", err)
		return 1
	}
	ingestDur := time.Since(ingestStart)

	recordStart := time.Now()
	if err := dbase.Q.RecordFetch(ctx, dbgen.RecordFetchParams{
		HourUtc:       hour.Unix(),
		PayloadSha256: sum,
		FetchedAt:     fetchedAt.Unix(),
	}); err != nil {
		log.Error("record fetch failed", "err", err)
		return 1
	}

	log.Info("fetch complete",
		"bytes", len(raw),
		"markets_seen", stats.MarketsSeen,
		"markets_inserted", stats.MarketsInserted,
		"markets_skipped", stats.MarketsSkipped,
		"new_currencies", len(stats.NewCurrencies),
		"fetch_dur", fetchDur,
		"decode_dur", decodeDur,
		"ingest_dur", ingestDur,
		"record_dur", time.Since(recordStart),
		"total_dur", time.Since(runStart),
	)
	return 0
}
