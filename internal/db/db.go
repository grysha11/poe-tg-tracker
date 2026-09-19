package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
)

type DB struct {
	*sql.DB
	Q *dbgen.Queries
}

func Open(dsn string) (*DB, error) {
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Minute)

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return &DB{DB: sqlDB, Q: dbgen.New(sqlDB)}, nil
}
