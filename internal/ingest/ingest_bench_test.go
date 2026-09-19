package ingest_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/db"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
	"github.com/grysha11/poe-tg-tracker/internal/ingest"
)

func newBenchDB(tb testing.TB) *db.DB {
	tb.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		tb.Fatal("TEST_DB_DSN env required (e.g. run: docker compose up -d mysql)")
	}

	dbase, err := db.Open(dsn)
	if err != nil {
		tb.Fatalf("open db: %v", err)
	}
	tb.Cleanup(func() { dbase.Close() })

	for _, table := range []string{"market_snapshots", "default_rate_pairs", "currencies"} {
		if _, err := dbase.Exec("TRUNCATE TABLE " + table); err != nil {
			tb.Fatalf("truncate %s: %v", table, err)
		}
	}

	return dbase
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func loadRealDigest(tb testing.TB) *exchange.Digest {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "real_digest.json"))
	if err != nil {
		tb.Fatalf("read real digest fixture: %v", err)
	}
	var d exchange.Digest
	if err := json.Unmarshal(data, &d); err != nil {
		tb.Fatalf("decode real digest fixture: %v", err)
	}
	return &d
}

func BenchmarkListCurrencies(b *testing.B) {
	dbase := newBenchDB(b)
	ctx := context.Background()
	log := silentLogger()
	digest := loadRealDigest(b)
	if _, err := ingest.Run(ctx, dbase, log, digest, time.Now(), time.Now()); err != nil {
		b.Fatalf("seed run: %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := dbase.Q.ListCurrencies(ctx); err != nil {
			b.Fatalf("list currencies: %v", err)
		}
	}
}

func BenchmarkRun_Warm(b *testing.B) {
	dbase := newBenchDB(b)
	ctx := context.Background()
	log := silentLogger()
	digest := loadRealDigest(b)

	baseHour := time.Now().Truncate(time.Hour)
	if _, err := ingest.Run(ctx, dbase, log, digest, baseHour, time.Now()); err != nil {
		b.Fatalf("warmup run: %v", err)
	}

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		hour := baseHour.Add(time.Duration(i) * time.Hour)
		if _, err := ingest.Run(ctx, dbase, log, digest, hour, time.Now()); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

func BenchmarkRun_Cold(b *testing.B) {
	ctx := context.Background()
	log := silentLogger()
	digest := loadRealDigest(b)
	baseHour := time.Now().Truncate(time.Hour)

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		b.StopTimer()
		dbase := newBenchDB(b)
		b.StartTimer()

		hour := baseHour.Add(time.Duration(i) * time.Hour)
		if _, err := ingest.Run(ctx, dbase, log, digest, hour, time.Now()); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}
