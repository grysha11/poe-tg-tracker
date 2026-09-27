package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

func main() {
	config.LoadDotEnv()

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	switch os.Args[1] {
	case "list":
		runList(ctx, mustDial())
	case "set":
		runSet(ctx, mustDial(), os.Args[2:])
	case "sync":
		runSync(ctx, mustDial(), os.Args[2:])
	case "bootstrap":
		runBootstrap(ctx, mustDial())
	default:
		usage()
		os.Exit(1)
	}
}

func mustDial() pb.ExchangeAdminServiceClient {
	addr := os.Getenv("EXCHANGE_SERVICE_ADDR")
	if addr == "" {
		fmt.Fprintln(os.Stderr, "curate: EXCHANGE_SERVICE_ADDR env required")
		os.Exit(1)
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "curate: dial %s failed: %v\n", addr, err)
		os.Exit(1)
	}
	return pb.NewExchangeAdminServiceClient(conn)
}

func fail(action string, err error) {
	fmt.Fprintf(os.Stderr, "curate: %s failed: %s\n", action, status.Convert(err).Message())
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `curate: manage auto-discovered placeholder currencies

Talks to exchange-service's admin API (EXCHANGE_SERVICE_ADDR), not the DB.

Usage:
  curate list
      List currencies still awaiting curation.

  curate set -path <item_path> -trade-id <trade_id> -name <name> [-emoji <emoji_id>]
      Curate a placeholder into a real currency.

  curate bootstrap
      Seed default_rate_pairs (Divine -> Chaos, Divine -> Exalt). Run
      "curate sync" first so the currencies exist. Safe to re-run.

  curate sync [-realm poe2] [-league "Forbidden Rites"]
      Upsert trade_id/name for every currency known to api.poe2scout.com,
      matched by item_path. League defaults to exchange-service's POE_LEAGUE.
      Safe to re-run.`)
}

func runList(ctx context.Context, client pb.ExchangeAdminServiceClient) {
	resp, err := client.ListPlaceholderCurrencies(ctx, &pb.ListPlaceholderCurrenciesRequest{})
	if err != nil {
		fail("list", err)
	}
	if len(resp.GetCurrencies()) == 0 {
		fmt.Println("no placeholder currencies pending curation")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tITEM_PATH\tPLACEHOLDER_NAME\tDISCOVERED_AT")
	for _, c := range resp.GetCurrencies() {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n",
			c.GetCurrencyId(), c.GetItemPath(), c.GetName(),
			time.Unix(c.GetDiscoveredAt(), 0).UTC().Format(time.RFC3339))
	}
	w.Flush()
}

func runSet(ctx context.Context, client pb.ExchangeAdminServiceClient, args []string) {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	path := fs.String("path", "", "currency item_path, e.g. Metadata/Items/Currency/CurrencyRerollRare")
	tradeID := fs.String("trade-id", "", "short trade id, e.g. chaos")
	name := fs.String("name", "", "display name, e.g. Chaos")
	emoji := fs.String("emoji", "", "emoji id from internal/emoji ids.json (optional)")
	fs.Parse(args)

	if *path == "" || *tradeID == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "curate: -path, -trade-id and -name are required")
		fs.Usage()
		os.Exit(1)
	}

	if _, err := client.CurateCurrency(ctx, &pb.CurateCurrencyRequest{
		ItemPath: *path,
		TradeId:  *tradeID,
		Name:     *name,
		EmojiId:  *emoji,
	}); err != nil {
		fail("set", err)
	}

	fmt.Printf("curated %s -> trade_id=%s name=%s\n", *path, *tradeID, *name)
}

func runSync(ctx context.Context, client pb.ExchangeAdminServiceClient, args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	realm := fs.String("realm", "poe2", "poe2scout realm")
	league := fs.String("league", "", "poe2scout league name (default: exchange-service's POE_LEAGUE)")
	fs.Parse(args)

	resp, err := client.SyncCurrenciesFromScout(ctx, &pb.SyncCurrenciesFromScoutRequest{Realm: *realm, League: *league})
	if err != nil {
		fail("sync", err)
	}

	fmt.Printf("synced %d currencies (%d skipped: no item_path)\n", resp.GetSyncedCount(), resp.GetSkippedCount())
}

func runBootstrap(ctx context.Context, client pb.ExchangeAdminServiceClient) {
	resp, err := client.BootstrapDefaultRatePairs(ctx, &pb.BootstrapDefaultRatePairsRequest{})
	if err != nil {
		fail("bootstrap", err)
	}

	for _, p := range resp.GetPairs() {
		fmt.Printf("default pair: %s -> %s\n", p.GetBase().GetName(), p.GetQuote().GetName())
	}
}
