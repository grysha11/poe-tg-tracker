package ingest_test

import (
	"context"
	"testing"
	"time"

	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
	"github.com/grysha11/poe-tg-tracker/internal/ingest"
)

func TestRun_NoOrphanCurrencies(t *testing.T) {
	dbase := newBenchDB(t)
	ctx := context.Background()
	digest := loadRealDigest(t)
	hour := time.Now().Truncate(time.Hour)

	stats, err := ingest.Run(ctx, dbase, silentLogger(), digest, hour, time.Now())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.MarketsInserted == 0 {
		t.Fatal("no markets inserted")
	}

	snapOrphans, err := dbase.Q.CountOrphanSnapshotCurrencies(ctx)
	if err != nil {
		t.Fatalf("count snapshot orphans: %v", err)
	}
	if snapOrphans != 0 {
		t.Errorf("market_snapshots has %d rows with a missing currency", snapOrphans)
	}

	pairOrphans, err := dbase.Q.CountOrphanRatePairCurrencies(ctx)
	if err != nil {
		t.Fatalf("count rate pair orphans: %v", err)
	}
	if pairOrphans != 0 {
		t.Errorf("default_rate_pairs has %d rows with a missing currency", pairOrphans)
	}
}

func TestResolveReadBack_SeesCurrencyCommittedAfterSnapshot(t *testing.T) {
	dbase := newBenchDB(t)
	ctx := context.Background()
	path := "Metadata/Items/Currency/CommittedConcurrently"

	tx, err := dbase.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	q := dbase.Q.WithTx(tx)

	if _, err := q.ListCurrencies(ctx); err != nil {
		t.Fatalf("establish snapshot: %v", err)
	}

	if err := dbase.Q.UpsertCurrencySynced(ctx, dbgen.UpsertCurrencySyncedParams{
		ItemPath: path, TradeID: "concurrent", Name: "Concurrent",
	}); err != nil {
		t.Fatalf("concurrent sync upsert: %v", err)
	}

	if err := q.UpsertCurrencyPlaceholder(ctx, dbgen.UpsertCurrencyPlaceholderParams{
		ItemPath: path, TradeID: "placeholder", Name: "placeholder",
	}); err != nil {
		t.Fatalf("placeholder upsert: %v", err)
	}

	c, err := q.GetCurrencyByPathForUpdate(ctx, path)
	if err != nil {
		t.Fatalf("read back after upsert: %v", err)
	}
	if c.TradeID != "concurrent" {
		t.Errorf("trade_id = %q, want the concurrently committed %q", c.TradeID, "concurrent")
	}
}

func TestRun_RerunSameHourIsIdempotent(t *testing.T) {
	dbase := newBenchDB(t)
	ctx := context.Background()
	digest := loadRealDigest(t)
	hour := time.Now().Truncate(time.Hour)

	if _, err := ingest.Run(ctx, dbase, silentLogger(), digest, hour, time.Now()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	var first int
	if err := dbase.QueryRow("SELECT COUNT(*) FROM market_snapshots").Scan(&first); err != nil {
		t.Fatal(err)
	}

	if _, err := ingest.Run(ctx, dbase, silentLogger(), digest, hour, time.Now()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var second int
	if err := dbase.QueryRow("SELECT COUNT(*) FROM market_snapshots").Scan(&second); err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Errorf("rerun changed row count: %d -> %d", first, second)
	}
	if orphans, _ := dbase.Q.CountOrphanSnapshotCurrencies(ctx); orphans != 0 {
		t.Errorf("orphans after rerun: %d", orphans)
	}
}
