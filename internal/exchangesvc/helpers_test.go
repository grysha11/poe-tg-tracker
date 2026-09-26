package exchangesvc_test

import (
	"context"
	"net"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/grysha11/poe-tg-tracker/internal/db"
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

func insertPlaceholder(t *testing.T, dbase *db.DB, itemPath string) {
	t.Helper()
	if _, err := dbase.Exec(
		`INSERT INTO currencies (item_path, trade_id, name, is_placeholder, discovered_at, updated_at) VALUES (?, ?, ?, 1, 0, 0)`,
		itemPath, itemPath, itemPath); err != nil {
		t.Fatalf("insert placeholder %s: %v", itemPath, err)
	}
}
