package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/XSAM/otelsql"
	"github.com/go-sql-driver/mysql"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
	"github.com/grysha11/poe-tg-tracker/internal/retry"
)

const defaultConnectTimeout = 60 * time.Second

var connectPolicy = retry.Policy{Attempts: 20, Base: 500 * time.Millisecond, Max: 5 * time.Second}

type DB struct {
	*sql.DB
	Q     *dbgen.Queries
	stats metric.Registration
}

func Open(dsn string) (*DB, error) {
	dbAttrs := []attribute.KeyValue{attribute.String("db.system.name", "mysql")}
	target := "unknown"
	if cfg, err := mysql.ParseDSN(dsn); err == nil {
		target = cfg.Addr + "/" + cfg.DBName
		dbAttrs = append(dbAttrs, attribute.String("db.namespace", cfg.DBName))
	}

	sqlDB, err := otelsql.Open("mysql", dsn,
		otelsql.WithAttributes(dbAttrs...),
		otelsql.WithInstrumentAttributesGetter(queryNameAttr),
	)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout())
	defer cancel()

	start := time.Now()
	attempts := 0
	err = retry.Do(ctx, "mysql", connectPolicy, func(ctx context.Context) error {
		attempts++
		err := sqlDB.PingContext(ctx)
		if _, isServerErr := errors.AsType[*mysql.MySQLError](err); err != nil && !isServerErr {
			return retry.Retryable(err)
		}
		return err
	})
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping mysql %s after %d attempts: %w", target, attempts, err)
	}
	slog.Info("mysql connected", "target", target, "attempts", attempts, "dur", time.Since(start))

	stats, err := otelsql.RegisterDBStatsMetrics(sqlDB, otelsql.WithAttributes(dbAttrs...))
	if err != nil {
		slog.Warn("register db stats metrics failed", "err", err)
	}

	return &DB{DB: sqlDB, Q: dbgen.New(sqlDB), stats: stats}, nil
}

func (d *DB) Close() error {
	if d.stats != nil {
		d.stats.Unregister()
	}
	return d.DB.Close()
}

func connectTimeout() time.Duration {
	if v := os.Getenv("DB_CONNECT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultConnectTimeout
}

func queryNameAttr(_ context.Context, _ otelsql.Method, query string, _ []driver.NamedValue) []attribute.KeyValue {
	if name := QueryName(query); name != "" {
		return []attribute.KeyValue{attribute.String("db.query.name", name)}
	}
	return nil
}

func QueryName(query string) string {
	rest, ok := strings.CutPrefix(query, "-- name: ")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, " ")
	return name
}
