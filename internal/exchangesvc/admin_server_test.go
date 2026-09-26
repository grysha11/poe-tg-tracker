package exchangesvc_test

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grysha11/poe-tg-tracker/internal/db"
	"github.com/grysha11/poe-tg-tracker/internal/exchangesvc"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
	"github.com/grysha11/poe-tg-tracker/internal/poe2scout"
)

type fakeScout struct {
	items         []poe2scout.CurrencyItem
	realm, league string
}

func (f *fakeScout) AllCurrencyItems(_ context.Context, realm, league string) ([]poe2scout.CurrencyItem, error) {
	f.realm, f.league = realm, league
	return f.items, nil
}

func newAdminClient(t *testing.T, dbase *db.DB, scout exchangesvc.ScoutClient) pb.ExchangeAdminServiceClient {
	t.Helper()
	conn := serveBufconn(t, func(srv *grpc.Server) {
		pb.RegisterExchangeAdminServiceServer(srv, &exchangesvc.AdminServer{Q: dbase.Q, Scout: scout, DefaultLeague: testLeague})
	})
	return pb.NewExchangeAdminServiceClient(conn)
}

func TestListPlaceholderCurrencies(t *testing.T) {
	dbase := newTestDB(t)
	insertPlaceholder(t, dbase, "Metadata/Items/SoulCores/Unknown")
	insertCurrency(t, dbase, "Metadata/Items/Currency/CurrencyRerollRare", "chaos", "Chaos Orb")
	client := newAdminClient(t, dbase, &fakeScout{})

	resp, err := client.ListPlaceholderCurrencies(context.Background(), &pb.ListPlaceholderCurrenciesRequest{})
	if err != nil {
		t.Fatalf("ListPlaceholderCurrencies: %v", err)
	}
	got := resp.GetCurrencies()
	if len(got) != 1 || got[0].GetItemPath() != "Metadata/Items/SoulCores/Unknown" {
		t.Errorf("placeholders = %v, want only the SoulCores one", got)
	}
}

func TestCurateCurrency(t *testing.T) {
	dbase := newTestDB(t)
	path := "Metadata/Items/SoulCores/Unknown"
	insertPlaceholder(t, dbase, path)
	client := newAdminClient(t, dbase, &fakeScout{})
	ctx := context.Background()

	t.Run("missing fields is InvalidArgument", func(t *testing.T) {
		_, err := client.CurateCurrency(ctx, &pb.CurateCurrencyRequest{ItemPath: path})
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("code = %v, want InvalidArgument (err: %v)", status.Code(err), err)
		}
	})

	t.Run("curates a placeholder", func(t *testing.T) {
		if _, err := client.CurateCurrency(ctx, &pb.CurateCurrencyRequest{
			ItemPath: path, TradeId: "soul-core", Name: "Soul Core", EmojiId: "e1",
		}); err != nil {
			t.Fatalf("CurateCurrency: %v", err)
		}
		c, err := dbase.Q.GetCurrencyByPath(ctx, path)
		if err != nil {
			t.Fatalf("GetCurrencyByPath: %v", err)
		}
		if c.IsPlaceholder || c.TradeID != "soul-core" || c.Name != "Soul Core" || c.EmojiID.String != "e1" {
			t.Errorf("currency = %+v, want curated soul-core with emoji e1", c)
		}
	})
}

func TestSyncCurrenciesFromScout(t *testing.T) {
	dbase := newTestDB(t)
	ctx := context.Background()
	chaosPath := "Metadata/Items/Currency/CurrencyRerollRare"

	if _, err := dbase.Exec(
		`INSERT INTO currencies (item_path, trade_id, name, emoji_id, is_placeholder, discovered_at, updated_at) VALUES (?, 'old', 'Old', 'e-chaos', 0, 0, 0)`,
		chaosPath); err != nil {
		t.Fatalf("seed chaos: %v", err)
	}

	scout := &fakeScout{items: []poe2scout.CurrencyItem{
		{ApiId: "chaos", Text: "Chaos Orb", BaseItemTypeId: new(chaosPath)},
		{ApiId: "chaos-dup", Text: "Chaos Dup", BaseItemTypeId: new(chaosPath)},
		{ApiId: "exalted", Text: "Exalted Orb", BaseItemTypeId: new("Metadata/Items/Currency/CurrencyAddModToRare")},
		{ApiId: "no-path", Text: "No Path"},
		{ApiId: "empty-path", Text: "Empty Path", BaseItemTypeId: new("")},
	}}
	client := newAdminClient(t, dbase, scout)

	resp, err := client.SyncCurrenciesFromScout(ctx, &pb.SyncCurrenciesFromScoutRequest{})
	if err != nil {
		t.Fatalf("SyncCurrenciesFromScout: %v", err)
	}
	if resp.GetSyncedCount() != 2 || resp.GetSkippedCount() != 2 {
		t.Errorf("synced/skipped = %d/%d, want 2/2", resp.GetSyncedCount(), resp.GetSkippedCount())
	}
	if scout.realm != "poe2" || scout.league != testLeague {
		t.Errorf("scout called with realm/league = %q/%q, want poe2/%q", scout.realm, scout.league, testLeague)
	}

	c, err := dbase.Q.GetCurrencyByPath(ctx, chaosPath)
	if err != nil {
		t.Fatalf("GetCurrencyByPath: %v", err)
	}
	if c.TradeID != "chaos" || c.Name != "Chaos Orb" {
		t.Errorf("chaos = %s/%s, want first-seen chaos/Chaos Orb", c.TradeID, c.Name)
	}
	if c.EmojiID.String != "e-chaos" {
		t.Errorf("chaos emoji = %q, want e-chaos (sync must not touch curated emoji)", c.EmojiID.String)
	}
}

func TestBootstrapDefaultRatePairs(t *testing.T) {
	dbase := newTestDB(t)
	client := newAdminClient(t, dbase, &fakeScout{})
	ctx := context.Background()

	t.Run("before sync is FailedPrecondition", func(t *testing.T) {
		_, err := client.BootstrapDefaultRatePairs(ctx, &pb.BootstrapDefaultRatePairsRequest{})
		if status.Code(err) != codes.FailedPrecondition {
			t.Errorf("code = %v, want FailedPrecondition (err: %v)", status.Code(err), err)
		}
	})

	insertCurrency(t, dbase, "Metadata/Items/Currency/CurrencyModValues", "divine", "Divine Orb")
	insertCurrency(t, dbase, "Metadata/Items/Currency/CurrencyRerollRare", "chaos", "Chaos Orb")
	insertCurrency(t, dbase, "Metadata/Items/Currency/CurrencyAddModToRare", "exalted", "Exalted Orb")

	t.Run("defaults seed divine->chaos, divine->exalted and are idempotent", func(t *testing.T) {
		for range 2 {
			resp, err := client.BootstrapDefaultRatePairs(ctx, &pb.BootstrapDefaultRatePairsRequest{})
			if err != nil {
				t.Fatalf("BootstrapDefaultRatePairs: %v", err)
			}
			pairs := resp.GetPairs()
			if len(pairs) != 2 || pairs[0].GetQuote().GetTradeId() != "chaos" || pairs[1].GetQuote().GetTradeId() != "exalted" {
				t.Fatalf("pairs = %v, want divine->chaos, divine->exalted", pairs)
			}
		}
		var n int
		if err := dbase.QueryRow("SELECT COUNT(*) FROM default_rate_pairs").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Errorf("default_rate_pairs rows = %d after two runs, want 2", n)
		}
	})
}
