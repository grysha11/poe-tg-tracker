package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
)

type DB struct {
	*sql.DB
	Q *dbgen.Queries
}

func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	sqlDB.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("set %q: %w", pragma, err)
		}
	}

	return &DB{DB: sqlDB, Q: dbgen.New(sqlDB)}, nil
}
