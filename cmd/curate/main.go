package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/db"
	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
	"github.com/grysha11/poe-tg-tracker/internal/poe2scout"
)

func main() {
	config.LoadDotEnv()

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	ctx := context.Background()

	switch os.Args[1] {
	case "list":
		dbase := mustOpenDB()
		defer dbase.Close()
		runList(ctx, dbase)
	case "set":
		dbase := mustOpenDB()
		defer dbase.Close()
		runSet(ctx, dbase, os.Args[2:])
	case "sync":
		dbase := mustOpenDB()
		defer dbase.Close()
		runSync(ctx, dbase, os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func mustOpenDB() *db.DB {
	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		fmt.Fprintln(os.Stderr, "curate: DB_DSN env required")
		os.Exit(1)
	}

	dbase, err := db.Open(dbDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curate: db open failed: %v\n", err)
		os.Exit(1)
	}

	return dbase
}

func usage() {
	fmt.Fprintln(os.Stderr, `curate: manage auto-discovered placeholder currencies

Usage:
  curate list
      List currencies still awaiting curation.

  curate set -path <item_path> -trade-id <trade_id> -name <name> [-emoji <emoji_id>]
      Curate a placeholder into a real currency.

  curate sync [-realm poe2] [-league "Forbidden Rites"]
      Upsert trade_id/name directly into the DB for every currency known to
      api.poe2scout.com, matched by item_path. Safe to re-run.`)
}

func runList(ctx context.Context, dbase *db.DB) {
	rows, err := dbase.Q.ListPlaceholderCurrencies(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curate: list failed: %v\n", err)
		os.Exit(1)
	}
	if len(rows) == 0 {
		fmt.Println("no placeholder currencies pending curation")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tITEM_PATH\tPLACEHOLDER_NAME\tDISCOVERED_AT")
	for _, r := range rows {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n",
			r.CurrencyID, r.ItemPath, r.Name,
			time.Unix(r.DiscoveredAt, 0).UTC().Format(time.RFC3339))
	}
	w.Flush()
}

func runSet(ctx context.Context, dbase *db.DB, args []string) {
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

	emojiID := sql.NullString{}
	if *emoji != "" {
		emojiID = sql.NullString{String: *emoji, Valid: true}
	}

	now := time.Now().Unix()
	if err := dbase.Q.UpsertCurrencyCurated(ctx, dbgen.UpsertCurrencyCuratedParams{
		ItemPath:     *path,
		TradeID:      *tradeID,
		Name:         *name,
		EmojiID:      emojiID,
		DiscoveredAt: now,
		UpdatedAt:    now,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "curate: set failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("curated %s -> trade_id=%s name=%s\n", *path, *tradeID, *name)
}

func runSync(ctx context.Context, dbase *db.DB, args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	realm := fs.String("realm", "poe2", "poe2scout realm")
	league := fs.String("league", "Forbidden Rites", "poe2scout league name")
	fs.Parse(args)

	userAgent := "poe-tg-tracker-curate/0.1.0"
	if contact := os.Getenv("POE_CONTACT"); contact != "" {
		userAgent = fmt.Sprintf("poe-tg-tracker-curate/0.1.0 (contact: %s)", contact)
	}

	items, err := poe2scout.NewClient(userAgent).AllCurrencyItems(ctx, *realm, *league)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curate: sync fetch failed: %v\n", err)
		os.Exit(1)
	}

	type row struct{ path, tradeID, name string }
	seen := make(map[string]bool, len(items))
	var rows []row
	skipped := 0
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
	if len(rows) == 0 {
		fmt.Println("curate: no currency items with an item_path found, nothing to sync")
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].path < rows[j].path })

	now := time.Now().Unix()
	for _, r := range rows {
		if err := dbase.Q.UpsertCurrencySynced(ctx, dbgen.UpsertCurrencySyncedParams{
			ItemPath:     r.path,
			TradeID:      r.tradeID,
			Name:         r.name,
			DiscoveredAt: now,
			UpdatedAt:    now,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "curate: sync upsert failed for %s: %v\n", r.path, err)
			os.Exit(1)
		}
	}

	fmt.Printf("synced %d currencies (%d skipped: no item_path)\n", len(rows), skipped)
}
