package db

import (
	"context"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"

	"github.com/grysha11/poe-tg-tracker/internal/db/migrations"
)

func Migrate(ctx context.Context, dsn string) ([]*goose.MigrationResult, int64, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, 0, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.ParseTime = true

	dbase, err := Open(cfg.FormatDSN())
	if err != nil {
		return nil, 0, err
	}
	defer dbase.Close()

	provider, err := goose.NewProvider(goose.DialectMySQL, dbase.DB, migrations.FS)
	if err != nil {
		return nil, 0, fmt.Errorf("new migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return results, 0, fmt.Errorf("run migrations: %w", err)
	}

	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return results, 0, fmt.Errorf("read db version: %w", err)
	}
	return results, version, nil
}
