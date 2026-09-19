package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/db"
	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
)

type Stats struct {
	MarketsSeen     int
	MarketsInserted int
	MarketsSkipped  int
	NewCurrencies   []string
}

func Run(ctx context.Context, dbase *db.DB, log *slog.Logger, digest *exchange.Digest, hour, fetchedAt time.Time) (Stats, error) {
	var stats Stats

	currencies, err := dbase.Q.ListCurrencies(ctx)
	if err != nil {
		return stats, fmt.Errorf("list currencies: %w", err)
	}
	byPath := make(map[string]dbgen.Currency, len(currencies))
	for _, c := range currencies {
		byPath[c.ItemPath] = c
	}

	tx, err := dbase.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	q := dbase.Q.WithTx(tx)
	hourUnix := hour.Unix()
	fetchedAtUnix := fetchedAt.Unix()

	resolve := func(itemPath string) (int64, error) {
		if c, ok := byPath[itemPath]; ok {
			return c.CurrencyID, nil
		}

		name := lastSegment(itemPath)
		if err := q.UpsertCurrencyPlaceholder(ctx, dbgen.UpsertCurrencyPlaceholderParams{
			ItemPath:     itemPath,
			TradeID:      name,
			Name:         name,
			DiscoveredAt: fetchedAtUnix,
			UpdatedAt:    fetchedAtUnix,
		}); err != nil {
			return 0, fmt.Errorf("upsert placeholder %q: %w", itemPath, err)
		}

		c, err := q.GetCurrencyByPath(ctx, itemPath)
		if err != nil {
			return 0, fmt.Errorf("get currency after upsert %q: %w", itemPath, err)
		}

		byPath[itemPath] = c
		stats.NewCurrencies = append(stats.NewCurrencies, itemPath)
		log.Warn("new currency discovered", "item_path", itemPath, "currency_id", c.CurrencyID)
		return c.CurrencyID, nil
	}

	for _, m := range digest.Markets {
		stats.MarketsSeen++

		if len(m.MarketPair) != 2 {
			stats.MarketsSkipped++
			log.Warn("skipping malformed market", "market_id", m.MarketID, "pair_len", len(m.MarketPair))
			continue
		}

		pathA, pathB := m.MarketPair[0], m.MarketPair[1]

		idA, err := resolve(pathA)
		if err != nil {
			return stats, err
		}
		idB, err := resolve(pathB)
		if err != nil {
			return stats, err
		}

		volA, volB := m.VolumeTraded[pathA], m.VolumeTraded[pathB]
		lowA, lowB := m.LowestRatio[pathA], m.LowestRatio[pathB]
		highA, highB := m.HighestRatio[pathA], m.HighestRatio[pathB]

		if idA > idB {
			idA, idB = idB, idA
			volA, volB = volB, volA
			lowA, lowB = lowB, lowA
			highA, highB = highB, highA
		}

		if err := q.InsertMarketSnapshot(ctx, dbgen.InsertMarketSnapshotParams{
			HourUtc:       hourUnix,
			League:        m.League,
			MarketID:      m.MarketID,
			ItemAID:       idA,
			ItemBID:       idB,
			VolumeA:       int64(volA),
			VolumeB:       int64(volB),
			LowestRatioA:  int64(lowA),
			LowestRatioB:  int64(lowB),
			HighestRatioA: int64(highA),
			HighestRatioB: int64(highB),
			FetchedAt:     fetchedAtUnix,
		}); err != nil {
			return stats, fmt.Errorf("insert snapshot %q: %w", m.MarketID, err)
		}
		stats.MarketsInserted++
	}

	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("commit: %w", err)
	}

	return stats, nil
}

func lastSegment(path string) string {
	idx := strings.LastIndexByte(path, '/')
	if idx == -1 {
		return path
	}
	return path[idx+1:]
}
