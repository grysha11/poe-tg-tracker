package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/db"
	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
)

func main() {
	config.LoadDotEnv()

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		fmt.Fprintln(os.Stderr, "curate: DB_PATH env required")
		os.Exit(1)
	}

	dbase, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curate: db open failed: %v\n", err)
		os.Exit(1)
	}
	defer dbase.Close()

	ctx := context.Background()

	switch os.Args[1] {
	case "list":
		runList(ctx, dbase)
	case "set":
		runSet(ctx, dbase, os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `curate: manage auto-discovered placeholder currencies

Usage:
  curate list
      List currencies still awaiting curation.

  curate set -path <item_path> -trade-id <trade_id> -name <name> [-emoji <emoji_id>]
      Curate a placeholder into a real currency.`)
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
