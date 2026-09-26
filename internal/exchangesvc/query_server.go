package exchangesvc

import (
	"context"
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
	Client        *exchange.Client
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

	var ranked []exchange.CurrencyRate
	if req.GetView() == pb.RateView_RATE_VIEW_PRICE {
		chaos, _ := quoteByTradeID(pairs, "chaos")
		exalt, _ := quoteByTradeID(pairs, "exalted")
		ranked = exchange.RankByPrice(rows, base, chaos, exalt, limit)
	} else {
		ranked = exchange.RankByVolume(rows, base, limit)
	}

	return &pb.GetRatesResponse{
		Base:    toCurrencyRef(base),
		Rates:   toRankedRates(ranked),
		HourUtc: hourUnix,
		League:  league,
	}, nil
}

func (s *QueryServer) ListLeagues(ctx context.Context, req *pb.ListLeaguesRequest) (*pb.ListLeaguesResponse, error) {
	hour := req.GetHourUtc()
	if hour == 0 {
		hour = exchange.AlignHour(time.Now()).Add(-time.Hour).Unix()
	}

	d, err := s.Client.Fetch(ctx, hour)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "fetch exchange digest: %v", err)
	}

	return &pb.ListLeaguesResponse{Leagues: exchange.Leagues(d)}, nil
}

func (s *QueryServer) ListDefaultRatePairs(ctx context.Context, _ *pb.ListDefaultRatePairsRequest) (*pb.ListDefaultRatePairsResponse, error) {
	pairs, err := s.loadRatePairs(ctx)
	if err != nil {
		return nil, err
	}
	return &pb.ListDefaultRatePairsResponse{Pairs: toDefaultRatePairs(pairs)}, nil
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

func quoteByTradeID(pairs []ratePair, tradeID string) (exchange.Currency, bool) {
	for _, p := range pairs {
		if p.quote.TradeID == tradeID {
			return p.quote, true
		}
	}
	return exchange.Currency{}, false
}
