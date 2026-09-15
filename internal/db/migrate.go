package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func (d *DB) Migrate() error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, d.DB, migrationsFS)
	if err != nil {
		return fmt.Errorf("new migration provider: %w", err)
	}

	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
