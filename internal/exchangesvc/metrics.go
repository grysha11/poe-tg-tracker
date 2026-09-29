package exchangesvc

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
)

const meterName = "poetracker/exchange-service"

var scoutSyncTotal metric.Int64Counter

func init() {
	var err error
	scoutSyncTotal, err = otel.Meter(meterName).Int64Counter("poetracker.scout_sync.currencies",
		metric.WithDescription("Currencies processed by SyncCurrenciesFromScout, by result (synced or skipped)."))
	if err != nil {
		otel.Handle(err)
	}
}

func RegisterDBGauges(q *dbgen.Queries, league string) (metric.Registration, error) {
	meter := otel.Meter(meterName)

	lastFetch, err := meter.Int64ObservableGauge("poetracker.last_fetch.timestamp",
		metric.WithUnit("s"),
		metric.WithDescription("Unix time of the last successful fetcher run (fetch_log.fetched_at)."))
	if err != nil {
		return nil, err
	}
	snapshotHour, err := meter.Int64ObservableGauge("poetracker.latest_snapshot.hour",
		metric.WithUnit("s"),
		metric.WithDescription("Unix time of the newest market snapshot hour for the league."))
	if err != nil {
		return nil, err
	}
	placeholders, err := meter.Int64ObservableGauge("poetracker.placeholder_currencies",
		metric.WithDescription("Currencies still awaiting curation."))
	if err != nil {
		return nil, err
	}

	leagueAttr := metric.WithAttributes(attribute.String("league", league))
	return meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		switch last, err := q.LatestFetch(ctx); {
		case err == nil:
			o.ObserveInt64(lastFetch, last.FetchedAt)
		case !errors.Is(err, sql.ErrNoRows):
			slog.WarnContext(ctx, "gauge: latest fetch query failed", "err", err)
		}

		if hour, err := q.LatestSnapshotHour(ctx, league); err != nil {
			slog.WarnContext(ctx, "gauge: latest snapshot hour query failed", "league", league, "err", err)
		} else if hour > 0 {
			o.ObserveInt64(snapshotHour, hour, leagueAttr)
		}

		if rows, err := q.ListPlaceholderCurrencies(ctx); err != nil {
			slog.WarnContext(ctx, "gauge: placeholder currencies query failed", "err", err)
		} else {
			o.ObserveInt64(placeholders, int64(len(rows)))
		}
		return nil
	}, lastFetch, snapshotHour, placeholders)
}
