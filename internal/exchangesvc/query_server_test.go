package exchangesvc_test

import (
	"context"
	"net"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/grysha11/poe-tg-tracker/internal/db"
	"github.com/grysha11/poe-tg-tracker/internal/exchangesvc"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

const (
	testLeague = "Test League"
	testHour   = int64(1_758_000_000)
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Fatal("TEST_DB_DSN env required (run: task test:db)")
	}

	dbase, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { dbase.Close() })

	for _, table := range []string{"market_snapshots", "default_rate_pairs", "currencies", "fetch_log"} {
		if _, err := dbase.Exec("TRUNCATE TABLE " + table); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	return dbase
}

func insertCurrency(t *testing.T, dbase *db.DB, itemPath, tradeID, name string) int64 {
	t.Helper()
	res, err := dbase.Exec(
		`INSERT INTO currencies (item_path, trade_id, name, is_placeholder, discovered_at, updated_at) VALUES (?, ?, ?, 0, 0, 0)`,
		itemPath, tradeID, name)
	if err != nil {
		t.Fatalf("insert currency %s: %v", tradeID, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func insertSnapshot(t *testing.T, dbase *db.DB, league string, hour, a, b, volA, volB int64) {
	t.Helper()
	_, err := dbase.Exec(
		`INSERT INTO market_snapshots
		   (hour_utc, league, market_id, item_a_id, item_b_id, volume_a, volume_b,
		    lowest_ratio_a, lowest_ratio_b, highest_ratio_a, highest_ratio_b, fetched_at)
		 VALUES (?, ?, CONCAT(?, '|', ?), ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		hour, league, a, b, a, b, volA, volB, volA, volB, volA, volB)
	if err != nil {
		t.Fatalf("insert snapshot %d/%d: %v", a, b, err)
	}
}

// seed mirrors a post-`curate bootstrap` DB with one fetched hour: divine is
// the base, chaos/exalt are the default quotes, mirror only trades vs divine.
func seed(t *testing.T, dbase *db.DB) {
	t.Helper()
	divine := insertCurrency(t, dbase, "Metadata/Items/Currency/CurrencyModValues", "divine", "Divine Orb")
	chaos := insertCurrency(t, dbase, "Metadata/Items/Currency/CurrencyRerollRare", "chaos", "Chaos Orb")
	exalt := insertCurrency(t, dbase, "Metadata/Items/Currency/CurrencyAddModToRare", "exalted", "Exalted Orb")
	mirror := insertCurrency(t, dbase, "Metadata/Items/Currency/CurrencyDuplicate", "mirror", "Mirror of Kalandra")

	for i, quote := range []int64{chaos, exalt} {
		if _, err := dbase.Exec(
			`INSERT INTO default_rate_pairs (base_currency_id, quote_currency_id, sort_order) VALUES (?, ?, ?)`,
			divine, quote, i); err != nil {
			t.Fatalf("insert rate pair: %v", err)
		}
	}

	insertSnapshot(t, dbase, testLeague, testHour, divine, chaos, 10, 1500)
	insertSnapshot(t, dbase, testLeague, testHour, divine, exalt, 30, 600)
	insertSnapshot(t, dbase, testLeague, testHour, divine, mirror, 50, 10)
}

func serveBufconn(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	register(srv)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func newClient(t *testing.T, dbase *db.DB) pb.ExchangeQueryServiceClient {
	t.Helper()
	conn := serveBufconn(t, func(srv *grpc.Server) {
		pb.RegisterExchangeQueryServiceServer(srv, &exchangesvc.QueryServer{Q: dbase.Q, DefaultLeague: testLeague})
	})
	return pb.NewExchangeQueryServiceClient(conn)
}

func TestListDefaultRatePairs(t *testing.T) {
	dbase := newTestDB(t)
	seed(t, dbase)
	client := newClient(t, dbase)

	resp, err := client.ListDefaultRatePairs(context.Background(), &pb.ListDefaultRatePairsRequest{})
	if err != nil {
		t.Fatalf("ListDefaultRatePairs: %v", err)
	}

	pairs := resp.GetPairs()
	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2", len(pairs))
	}
	if pairs[0].GetBase().GetTradeId() != "divine" || pairs[0].GetQuote().GetTradeId() != "chaos" {
		t.Errorf("pairs[0] = %s->%s, want divine->chaos", pairs[0].GetBase().GetTradeId(), pairs[0].GetQuote().GetTradeId())
	}
	if pairs[1].GetQuote().GetTradeId() != "exalted" || pairs[1].GetSortOrder() != 1 {
		t.Errorf("pairs[1] = quote %s sort %d, want exalted sort 1", pairs[1].GetQuote().GetTradeId(), pairs[1].GetSortOrder())
	}
}

func TestGetRates(t *testing.T) {
	dbase := newTestDB(t)
	seed(t, dbase)
	client := newClient(t, dbase)
	ctx := context.Background()

	t.Run("volume view, empty league falls back to default", func(t *testing.T) {
		resp, err := client.GetRates(ctx, &pb.GetRatesRequest{})
		if err != nil {
			t.Fatalf("GetRates: %v", err)
		}
		if resp.GetLeague() != testLeague || resp.GetHourUtc() != testHour {
			t.Errorf("league/hour = %q/%d, want %q/%d", resp.GetLeague(), resp.GetHourUtc(), testLeague, testHour)
		}
		if resp.GetBase().GetTradeId() != "divine" {
			t.Errorf("base = %s, want divine", resp.GetBase().GetTradeId())
		}
		var got []string
		for _, r := range resp.GetRates() {
			got = append(got, r.GetCurrency().GetTradeId())
		}
		want := []string{"mirror", "exalted", "chaos"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Errorf("volume order = %v, want %v", got, want)
		}
	})

	t.Run("price view", func(t *testing.T) {
		resp, err := client.GetRates(ctx, &pb.GetRatesRequest{View: pb.RateView_RATE_VIEW_PRICE})
		if err != nil {
			t.Fatalf("GetRates: %v", err)
		}
		rates := resp.GetRates()
		if len(rates) == 0 || rates[0].GetCurrency().GetTradeId() != "mirror" {
			t.Fatalf("first price-ranked = %v, want mirror", rates)
		}
		if rates[0].GetVwap() != 0.2 {
			t.Errorf("mirror vwap = %v, want 0.2", rates[0].GetVwap())
		}
	})

	t.Run("limit", func(t *testing.T) {
		resp, err := client.GetRates(ctx, &pb.GetRatesRequest{Limit: 1})
		if err != nil {
			t.Fatalf("GetRates: %v", err)
		}
		if len(resp.GetRates()) != 1 {
			t.Errorf("len(rates) = %d, want 1", len(resp.GetRates()))
		}
	})

	t.Run("unknown league is NotFound", func(t *testing.T) {
		_, err := client.GetRates(ctx, &pb.GetRatesRequest{League: "No Such League"})
		if status.Code(err) != codes.NotFound {
			t.Errorf("code = %v, want NotFound (err: %v)", status.Code(err), err)
		}
	})
}

func TestGetRates_NoDefaultPairsIsFailedPrecondition(t *testing.T) {
	dbase := newTestDB(t)
	client := newClient(t, dbase)

	_, err := client.GetRates(context.Background(), &pb.GetRatesRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition (err: %v)", status.Code(err), err)
	}
}
