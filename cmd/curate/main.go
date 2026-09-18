package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
		runSync(ctx, os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func mustOpenDB() *db.DB {
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

	if err := dbase.Migrate(); err != nil {
		fmt.Fprintf(os.Stderr, "curate: db migrate failed: %v\n", err)
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

  curate sync [-realm poe2] [-league "Forbidden Rites"] [-migrations-dir internal/db/migrations]
      Generate a goose migration seeding trade_id/name for every currency known
      to api.poe2scout.com, matched by item_path. Review the generated file,
      commit it, then apply it the normal way. Run locally from the repo root,
      not inside the deployed container — it only writes a .sql file, no DB.`)
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

func runSync(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	realm := fs.String("realm", "poe2", "poe2scout realm")
	league := fs.String("league", "Forbidden Rites", "poe2scout league name")
	migrationsDir := fs.String("migrations-dir", "internal/db/migrations", "path to the goose migrations directory (run from repo root)")
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
		fmt.Println("curate: no currency items with an item_path found, nothing to generate")
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].path < rows[j].path })

	seq, err := nextMigrationSeq(*migrationsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curate: %v\n", err)
		os.Exit(1)
	}

	now := time.Now().Unix()
	var b strings.Builder
	fmt.Fprintf(&b, "-- +goose Up\n-- generated by `curate sync` from api.poe2scout.com (realm=%s, league=%s)\n", *realm, *league)
	fmt.Fprintln(&b, "INSERT INTO currencies (item_path, trade_id, name, emoji_id, is_placeholder, discovered_at, updated_at) VALUES")
	for i, r := range rows {
		sep := ","
		if i == len(rows)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "    ('%s', '%s', '%s', NULL, 0, %d, %d)%s\n",
			sqlEscape(r.path), sqlEscape(r.tradeID), sqlEscape(r.name), now, now, sep)
	}
	fmt.Fprintln(&b, "ON CONFLICT (item_path) DO UPDATE SET")
	fmt.Fprintln(&b, "    trade_id       = excluded.trade_id,")
	fmt.Fprintln(&b, "    name           = excluded.name,")
	fmt.Fprintln(&b, "    is_placeholder = 0,")
	fmt.Fprintln(&b, "    updated_at     = excluded.updated_at;")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "-- +goose Down")
	fmt.Fprintln(&b, "-- data-only curation upsert; not meaningfully reversible without clobbering")
	fmt.Fprintln(&b, "-- whatever placeholder state existed before it ran.")
	fmt.Fprintln(&b, "SELECT 1;")

	filename := fmt.Sprintf("%05d_sync_poe2scout_currencies.sql", seq)
	outPath := filepath.Join(*migrationsDir, filename)
	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "curate: write migration failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s (%d currencies, %d skipped: no item_path)\n", outPath, len(rows), skipped)
	fmt.Println("review it, then apply the normal way (bot startup runs Migrate() on next deploy, or `goose up` directly).")
}

func nextMigrationSeq(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read migrations dir %q: %w", dir, err)
	}

	max := 0
	for _, e := range entries {
		name := e.Name()
		if len(name) < 5 {
			continue
		}
		n, err := strconv.Atoi(name[:5])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

func sqlEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
