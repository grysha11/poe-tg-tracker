package exchangesvc

import (
	"context"
	"fmt"
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

	// Loaded per request rather than once at startup (as bot used to): this
	// process is long-lived and curate can change default_rate_pairs under it.
	base, quotes, err := s.loadDefaultRates(ctx)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "load default rate pairs: %v", err)
	}

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
		chaos, _ := quoteByTradeID(quotes, "chaos")
		exalt, _ := quoteByTradeID(quotes, "exalted")
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
	rows, err := s.Q.ListDefaultRatePairs(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list default rate pairs: %v", err)
	}
	if err := checkRatePairRows(rows); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	pairs := make([]*pb.DefaultRatePair, 0, len(rows))
	for _, r := range rows {
		pairs = append(pairs, &pb.DefaultRatePair{
			Base:      &pb.CurrencyRef{ItemPath: r.BaseItemPath.String, Name: r.BaseName.String, TradeId: r.BaseTradeID.String},
			Quote:     &pb.CurrencyRef{ItemPath: r.QuoteItemPath.String, Name: r.QuoteName.String, TradeId: r.QuoteTradeID.String},
			SortOrder: int32(r.SortOrder),
		})
	}

	return &pb.ListDefaultRatePairsResponse{Pairs: pairs}, nil
}

func (s *QueryServer) loadDefaultRates(ctx context.Context) (exchange.Currency, []exchange.Currency, error) {
	rows, err := s.Q.ListDefaultRatePairs(ctx)
	if err != nil {
		return exchange.Currency{}, nil, err
	}
	if len(rows) == 0 {
		return exchange.Currency{}, nil, fmt.Errorf("no default rate pairs configured")
	}
	if err := checkRatePairRows(rows); err != nil {
		return exchange.Currency{}, nil, err
	}

	base := exchange.Currency{ID: rows[0].BaseItemPath.String, Name: rows[0].BaseName.String, TradeID: rows[0].BaseTradeID.String}
	quotes := make([]exchange.Currency, 0, len(rows))
	for _, r := range rows {
		quotes = append(quotes, exchange.Currency{ID: r.QuoteItemPath.String, Name: r.QuoteName.String, TradeID: r.QuoteTradeID.String})
	}
	return base, quotes, nil
}

func checkRatePairRows(rows []dbgen.ListDefaultRatePairsRow) error {
	for _, r := range rows {
		if !r.BaseItemPath.Valid || !r.QuoteItemPath.Valid {
			return fmt.Errorf(
				"default_rate_pairs row (base_currency_id=%d, quote_currency_id=%d) references a missing currency",
				r.BaseCurrencyID, r.QuoteCurrencyID)
		}
	}
	return nil
}

func toSnapshotRows(rows []dbgen.ListSnapshotRatesForHourRow) []exchange.SnapshotRow {
	out := make([]exchange.SnapshotRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, exchange.SnapshotRow{
			ItemA:         exchange.Currency{ID: r.ItemAPath, Name: r.ItemAName, TradeID: r.ItemATradeID},
			ItemB:         exchange.Currency{ID: r.ItemBPath, Name: r.ItemBName, TradeID: r.ItemBTradeID},
			VolumeA:       uint64(r.VolumeA),
			VolumeB:       uint64(r.VolumeB),
			LowestRatioA:  uint64(r.LowestRatioA),
			LowestRatioB:  uint64(r.LowestRatioB),
			HighestRatioA: uint64(r.HighestRatioA),
			HighestRatioB: uint64(r.HighestRatioB),
		})
	}
	return out
}

func quoteByTradeID(quotes []exchange.Currency, tradeID string) (exchange.Currency, bool) {
	for _, q := range quotes {
		if q.TradeID == tradeID {
			return q, true
		}
	}
	return exchange.Currency{}, false
}

func toCurrencyRef(c exchange.Currency) *pb.CurrencyRef {
	return &pb.CurrencyRef{ItemPath: c.ID, Name: c.Name, TradeId: c.TradeID}
}

func toRankedRates(ranked []exchange.CurrencyRate) []*pb.RankedRate {
	out := make([]*pb.RankedRate, 0, len(ranked))
	for _, cr := range ranked {
		rr := &pb.RankedRate{
			Currency:    toCurrencyRef(cr.Currency),
			Vwap:        cr.Rate.VWAP,
			Low:         cr.Rate.Low,
			High:        cr.Rate.High,
			BaseVolume:  cr.Rate.BaseVol,
			QuoteVolume: cr.Rate.QuoteVol,
		}
		if cr.Via != nil {
			rr.Via = toCurrencyRef(*cr.Via)
		}
		out = append(out, rr)
	}
	return out
}
