package exchangesvc

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

const defaultRatesLimit = 10

type QueryServer struct {
	pb.UnimplementedExchangeQueryServiceServer

	Q             *dbgen.Queries
	Digests       *exchange.Client
	DefaultLeague string
}

func (s *QueryServer) GetRates(ctx context.Context, req *pb.GetRatesRequest) (*pb.GetRatesResponse, error) {
	league := req.GetLeague()
	if league == "" {
		league = s.DefaultLeague
	}

	pairs, err := s.loadRatePairs(ctx)
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "no default rate pairs configured")
	}
	base := pairs[0].base

	hourUnix, err := s.Q.LatestSnapshotHour(ctx, league)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "latest snapshot hour: %v", err)
	}
	if hourUnix == 0 {
		return nil, status.Errorf(codes.NotFound, "no snapshots yet for league %q", league)
	}

	dbRows, err := s.Q.ListSnapshotRatesForHour(ctx, dbgen.ListSnapshotRatesForHourParams{
		HourUtc: hourUnix,
		League:  league,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list snapshot rates: %v", err)
	}
	rows := toSnapshotRows(dbRows)

	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = defaultRatesLimit
	}

	var lastFetchUnix int64
	switch last, err := s.Q.LatestFetch(ctx); {
	case err == nil:
		lastFetchUnix = last.FetchedAt
	case !errors.Is(err, sql.ErrNoRows):
		return nil, status.Errorf(codes.Internal, "latest fetch: %v", err)
	}

	var cats exchange.Categories
	if len(req.GetCategories()) > 0 {
		cats = exchange.Categories{}
		for _, c := range req.GetCategories() {
			cats[c] = true
		}
	}

	quotes := make([]exchange.Currency, 0, len(pairs))
	for _, p := range pairs {
		quotes = append(quotes, p.quote)
	}

	var ranked []exchange.CurrencyRate
	if req.GetView() == pb.RateView_RATE_VIEW_PRICE {
		ranked = exchange.RankByPrice(rows, base, quotes, limit, cats)
	} else {
		ranked = exchange.RankByVolume(rows, base, limit, cats)
	}

	prevHour, prev, err := s.previousRates(ctx, league, hourUnix, base, quotes)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "rates resolved",
		"league", league, "view", req.GetView().String(), "hour_utc", hourUnix, "prev_hour_utc", prevHour,
		"snapshot_rows", len(rows), "ranked", len(ranked), "limit", limit, "categories", req.GetCategories(), "last_fetch_utc", lastFetchUnix)

	return &pb.GetRatesResponse{
		Base:         toCurrencyRef(base),
		Rates:        toRankedRates(ranked, prev),
		HourUtc:      hourUnix,
		League:       league,
		LastFetchUtc: lastFetchUnix,
		PrevHourUtc:  prevHour,
	}, nil
}

// previousRates prices every currency in the latest snapshot hour before hour,
// however long ago that was.
func (s *QueryServer) previousRates(ctx context.Context, league string, hour int64, base exchange.Currency, quotes []exchange.Currency) (int64, map[string]exchange.CurrencyRate, error) {
	prevHour, err := s.Q.PreviousSnapshotHour(ctx, dbgen.PreviousSnapshotHourParams{League: league, HourUtc: hour})
	if err != nil {
		return 0, nil, status.Errorf(codes.Internal, "previous snapshot hour: %v", err)
	}
	if prevHour == 0 {
		return 0, nil, nil
	}

	dbRows, err := s.Q.ListSnapshotRatesForHour(ctx, dbgen.ListSnapshotRatesForHourParams{HourUtc: prevHour, League: league})
	if err != nil {
		return 0, nil, status.Errorf(codes.Internal, "list previous snapshot rates: %v", err)
	}
	return prevHour, exchange.RatesByID(toSnapshotRows(dbRows), base, quotes), nil
}

func (s *QueryServer) ListLeagues(ctx context.Context, req *pb.ListLeaguesRequest) (*pb.ListLeaguesResponse, error) {
	hour := req.GetHourUtc()
	if hour == 0 {
		hour = exchange.AlignHour(time.Now()).Add(-time.Hour).Unix()
	}

	d, err := s.Digests.Fetch(ctx, hour)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "fetch exchange digest: %v", err)
	}

	leagues := exchange.Leagues(d)
	slog.DebugContext(ctx, "leagues resolved", "hour_utc", hour, "markets", len(d.Markets), "leagues", len(leagues))
	return &pb.ListLeaguesResponse{Leagues: leagues}, nil
}

func (s *QueryServer) ListDefaultRatePairs(ctx context.Context, _ *pb.ListDefaultRatePairsRequest) (*pb.ListDefaultRatePairsResponse, error) {
	pairs, err := s.loadRatePairs(ctx)
	if err != nil {
		return nil, err
	}
	return &pb.ListDefaultRatePairsResponse{Pairs: toDefaultRatePairs(pairs)}, nil
}

func (s *QueryServer) ListCategories(ctx context.Context, _ *pb.ListCategoriesRequest) (*pb.ListCategoriesResponse, error) {
	rows, err := s.Q.ListCategories(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list categories: %v", err)
	}

	cats := make([]string, 0, len(rows))
	uncategorized := false
	for _, r := range rows {
		c := categoryFromDB(r)
		if c == exchange.Uncategorized {
			uncategorized = true
			continue
		}
		cats = append(cats, c)
	}
	if uncategorized {
		cats = append(cats, exchange.Uncategorized)
	}
	return &pb.ListCategoriesResponse{Categories: cats}, nil
}

// loadRatePairs returns gRPC status errors so handlers can pass them through.
func (s *QueryServer) loadRatePairs(ctx context.Context) ([]ratePair, error) {
	rows, err := s.Q.ListDefaultRatePairs(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list default rate pairs: %v", err)
	}

	pairs := make([]ratePair, 0, len(rows))
	for _, r := range rows {
		if !r.BaseItemPath.Valid || !r.QuoteItemPath.Valid {
			return nil, status.Errorf(codes.FailedPrecondition,
				"default_rate_pairs row (base_currency_id=%d, quote_currency_id=%d) references a missing currency",
				r.BaseCurrencyID, r.QuoteCurrencyID)
		}
		pairs = append(pairs, ratePairFromDB(r))
	}
	return pairs, nil
}
