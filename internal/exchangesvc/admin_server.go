package exchangesvc

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
	"github.com/grysha11/poe-tg-tracker/internal/poe2scout"
)

const (
	defaultScoutRealm = "poe2"
	divinePath        = "Metadata/Items/Currency/CurrencyModValues"
)

var defaultQuotePaths = []string{
	"Metadata/Items/Currency/CurrencyRerollRare",   // Chaos
	"Metadata/Items/Currency/CurrencyAddModToRare", // Exalt
}

type ScoutClient interface {
	AllCurrencyItems(ctx context.Context, realm, league string) ([]poe2scout.CurrencyItem, error)
}

type AdminServer struct {
	pb.UnimplementedExchangeAdminServiceServer

	Q             *dbgen.Queries
	Scout         ScoutClient
	DefaultLeague string
}

func (s *AdminServer) ListPlaceholderCurrencies(ctx context.Context, _ *pb.ListPlaceholderCurrenciesRequest) (*pb.ListPlaceholderCurrenciesResponse, error) {
	rows, err := s.Q.ListPlaceholderCurrencies(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list placeholder currencies: %v", err)
	}

	out := make([]*pb.PlaceholderCurrency, 0, len(rows))
	for _, r := range rows {
		out = append(out, &pb.PlaceholderCurrency{
			CurrencyId:   r.CurrencyID,
			ItemPath:     r.ItemPath,
			Name:         r.Name,
			DiscoveredAt: r.DiscoveredAt,
		})
	}
	return &pb.ListPlaceholderCurrenciesResponse{Currencies: out}, nil
}

func (s *AdminServer) CurateCurrency(ctx context.Context, req *pb.CurateCurrencyRequest) (*pb.CurateCurrencyResponse, error) {
	if req.GetItemPath() == "" || req.GetTradeId() == "" || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "item_path, trade_id and name are required")
	}

	emojiID := sql.NullString{}
	if req.GetEmojiId() != "" {
		emojiID = sql.NullString{String: req.GetEmojiId(), Valid: true}
	}

	now := time.Now().Unix()
	if err := s.Q.UpsertCurrencyCurated(ctx, dbgen.UpsertCurrencyCuratedParams{
		ItemPath:     req.GetItemPath(),
		TradeID:      req.GetTradeId(),
		Name:         req.GetName(),
		EmojiID:      emojiID,
		DiscoveredAt: now,
		UpdatedAt:    now,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "upsert curated currency: %v", err)
	}
	return &pb.CurateCurrencyResponse{}, nil
}

func (s *AdminServer) SyncCurrenciesFromScout(ctx context.Context, req *pb.SyncCurrenciesFromScoutRequest) (*pb.SyncCurrenciesFromScoutResponse, error) {
	realm := req.GetRealm()
	if realm == "" {
		realm = defaultScoutRealm
	}
	league := req.GetLeague()
	if league == "" {
		league = s.DefaultLeague
	}

	items, err := s.Scout.AllCurrencyItems(ctx, realm, league)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "fetch poe2scout currencies: %v", err)
	}

	type row struct{ path, tradeID, name string }
	seen := make(map[string]bool, len(items))
	var rows []row
	var skipped int32
	for _, item := range items {
		if item.BaseItemTypeId == nil || *item.BaseItemTypeId == "" {
			skipped++
			continue
		}
		path := *item.BaseItemTypeId
		if seen[path] {
			continue
		}
		seen[path] = true
		rows = append(rows, row{path: path, tradeID: item.ApiId, name: item.Text})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].path < rows[j].path })

	now := time.Now().Unix()
	for _, r := range rows {
		if err := s.Q.UpsertCurrencySynced(ctx, dbgen.UpsertCurrencySyncedParams{
			ItemPath:     r.path,
			TradeID:      r.tradeID,
			Name:         r.name,
			DiscoveredAt: now,
			UpdatedAt:    now,
		}); err != nil {
			return nil, status.Errorf(codes.Internal, "upsert synced currency %s: %v", r.path, err)
		}
	}

	return &pb.SyncCurrenciesFromScoutResponse{SyncedCount: int32(len(rows)), SkippedCount: skipped}, nil
}

func (s *AdminServer) BootstrapDefaultRatePairs(ctx context.Context, req *pb.BootstrapDefaultRatePairsRequest) (*pb.BootstrapDefaultRatePairsResponse, error) {
	basePath := req.GetBaseItemPath()
	if basePath == "" {
		basePath = divinePath
	}
	quotePaths := req.GetQuoteItemPaths()
	if len(quotePaths) == 0 {
		quotePaths = defaultQuotePaths
	}

	base, err := s.currencyByPath(ctx, basePath)
	if err != nil {
		return nil, err
	}

	pairs := make([]ratePair, 0, len(quotePaths))
	for i, path := range quotePaths {
		quote, err := s.currencyByPath(ctx, path)
		if err != nil {
			return nil, err
		}
		if err := s.Q.UpsertDefaultRatePair(ctx, dbgen.UpsertDefaultRatePairParams{
			BaseCurrencyID:  base.CurrencyID,
			QuoteCurrencyID: quote.CurrencyID,
			SortOrder:       int64(i),
		}); err != nil {
			return nil, status.Errorf(codes.Internal, "upsert default rate pair %s: %v", path, err)
		}
		pairs = append(pairs, ratePair{base: currencyFromDB(base), quote: currencyFromDB(quote), sortOrder: int32(i)})
	}
	return &pb.BootstrapDefaultRatePairsResponse{Pairs: toDefaultRatePairs(pairs)}, nil
}

func (s *AdminServer) currencyByPath(ctx context.Context, path string) (dbgen.Currency, error) {
	c, err := s.Q.GetCurrencyByPath(ctx, path)
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.Currency{}, status.Errorf(codes.FailedPrecondition, "currency %s not found (run sync first)", path)
	}
	if err != nil {
		return dbgen.Currency{}, status.Errorf(codes.Internal, "get currency %s: %v", path, err)
	}
	return c, nil
}
