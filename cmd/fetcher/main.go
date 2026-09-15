package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
	"github.com/grysha11/poe-tg-tracker/internal/logger"
)

var backoff = []time.Duration{0, 2 * time.Second, 5 * time.Second}

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

	var (
		raw []byte
		err error
	)
	for attempt, wait := range backoff {
		if wait > 0 {
			time.Sleep(wait)
		}
		raw, err = client.FetchRaw(ctx, hour.Unix())
		if err == nil {
			break
		}
		log.Warn("fetch attempt failed", "attempt", attempt+1, "err", err)
	}
	if err != nil {
		log.Error("fetch failed, giving up", "hour", hour.Format(time.RFC3339), "err", err)
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

	log.Info("fetch complete", "hour", hour.Format(time.RFC3339), "bytes", len(raw), "out", outPath)
}
