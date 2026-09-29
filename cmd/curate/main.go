package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"text/tabwriter"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/grpcclient"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
	"github.com/grysha11/poe-tg-tracker/internal/telemetry"
)

var (
	commands   = []string{"list", "set", "sync", "bootstrap"}
	errBadArgs = errors.New("bad arguments")
)

func main() {
	os.Exit(run())
}

func run() int {
	config.LoadDotEnv()

	if len(os.Args) < 2 || !slices.Contains(commands, os.Args[1]) {
		usage()
		return 1
	}
	command := os.Args[1]

	addr := os.Getenv("EXCHANGE_SERVICE_ADDR")
	if addr == "" {
		fmt.Fprintln(os.Stderr, "curate: EXCHANGE_SERVICE_ADDR env required")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tel, err := telemetry.Setup(ctx, "curate", telemetry.WithLogWriter(os.Stderr))
	if err != nil {
		fmt.Fprintf(os.Stderr, "curate: telemetry setup failed: %v\n", err)
		return 1
	}
	defer tel.ShutdownWithTimeout()
	log := tel.Log.With("command", command)

	conn, err := grpcclient.Dial(addr, grpc.WithDefaultCallOptions(grpc.WaitForReady(true)))
	if err != nil {
		log.Error("dial exchange-service failed", "addr", addr, "err", err)
		return 1
	}
	defer conn.Close()
	client := pb.NewExchangeAdminServiceClient(conn)

	start := time.Now()
	switch command {
	case "list":
		err = runList(ctx, client)
	case "set":
		err = runSet(ctx, client, os.Args[2:])
	case "sync":
		err = runSync(ctx, client, os.Args[2:])
	case "bootstrap":
		err = runBootstrap(ctx, client)
	}

	switch {
	case errors.Is(err, errBadArgs):
		return 1
	case err != nil:
		log.Error("curate command failed", "addr", addr, "code", status.Code(err).String(), "err", err, "dur", time.Since(start))
		fmt.Fprintf(os.Stderr, "curate: %s failed: %s\n", command, status.Convert(err).Message())
		return 1
	}
	log.Info("curate command ok", "addr", addr, "dur", time.Since(start))
	return 0
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

func runList(ctx context.Context, client pb.ExchangeAdminServiceClient) error {
	resp, err := client.ListPlaceholderCurrencies(ctx, &pb.ListPlaceholderCurrenciesRequest{})
	if err != nil {
		return err
	}
	if len(resp.GetCurrencies()) == 0 {
		fmt.Println("no placeholder currencies pending curation")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tITEM_PATH\tPLACEHOLDER_NAME\tDISCOVERED_AT")
	for _, c := range resp.GetCurrencies() {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n",
			c.GetCurrencyId(), c.GetItemPath(), c.GetName(),
			time.Unix(c.GetDiscoveredAt(), 0).UTC().Format(time.RFC3339))
	}
	return w.Flush()
}

func runSet(ctx context.Context, client pb.ExchangeAdminServiceClient, args []string) error {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	path := fs.String("path", "", "currency item_path, e.g. Metadata/Items/Currency/CurrencyRerollRare")
	tradeID := fs.String("trade-id", "", "short trade id, e.g. chaos")
	name := fs.String("name", "", "display name, e.g. Chaos")
	emoji := fs.String("emoji", "", "emoji id from internal/emoji ids.json (optional)")
	fs.Parse(args)

	if *path == "" || *tradeID == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "curate: -path, -trade-id and -name are required")
		fs.Usage()
		return errBadArgs
	}

	if _, err := client.CurateCurrency(ctx, &pb.CurateCurrencyRequest{
		ItemPath: *path,
		TradeId:  *tradeID,
		Name:     *name,
		EmojiId:  *emoji,
	}); err != nil {
		return err
	}

	fmt.Printf("curated %s -> trade_id=%s name=%s\n", *path, *tradeID, *name)
	return nil
}

func runSync(ctx context.Context, client pb.ExchangeAdminServiceClient, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	realm := fs.String("realm", "poe2", "poe2scout realm")
	league := fs.String("league", "", "poe2scout league name (default: exchange-service's POE_LEAGUE)")
	fs.Parse(args)

	resp, err := client.SyncCurrenciesFromScout(ctx, &pb.SyncCurrenciesFromScoutRequest{Realm: *realm, League: *league})
	if err != nil {
		return err
	}

	fmt.Printf("synced %d currencies (%d skipped: no item_path)\n", resp.GetSyncedCount(), resp.GetSkippedCount())
	return nil
}

func runBootstrap(ctx context.Context, client pb.ExchangeAdminServiceClient) error {
	resp, err := client.BootstrapDefaultRatePairs(ctx, &pb.BootstrapDefaultRatePairsRequest{})
	if err != nil {
		return err
	}

	for _, p := range resp.GetPairs() {
		fmt.Printf("default pair: %s -> %s\n", p.GetBase().GetName(), p.GetQuote().GetName())
	}
	return nil
}
