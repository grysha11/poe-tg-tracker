package db

import (
	"context"
	"testing"

	"github.com/XSAM/otelsql"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	listQuery   = "-- name: ListCurrencies :many\nSELECT 1"
	insertQuery = "-- name: InsertMarketSnapshot :exec\nINSERT INTO market_snapshots VALUES (?)"
)

func TestQueryName(t *testing.T) {
	for query, want := range map[string]string{
		listQuery:  "ListCurrencies",
		"SELECT 1": "",
		"":         "",
	} {
		if got := QueryName(query); got != want {
			t.Errorf("QueryName(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestSpanName(t *testing.T) {
	if got := spanName(context.Background(), otelsql.MethodConnQuery, listQuery); got != "ListCurrencies" {
		t.Errorf("spanName = %q, want the sqlc query name", got)
	}
	if got := spanName(context.Background(), otelsql.MethodTxCommit, ""); got != string(otelsql.MethodTxCommit) {
		t.Errorf("spanName = %q, want the method for unnamed statements", got)
	}
}

func TestSpanFilter(t *testing.T) {
	inSpan, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "op")
	defer span.End()

	if spanFilter(context.Background(), otelsql.MethodConnQuery, listQuery, nil) {
		t.Error("spanFilter without a parent span = true, want false so background queries don't start traces")
	}
	if !spanFilter(inSpan, otelsql.MethodConnQuery, listQuery, nil) {
		t.Error("spanFilter inside a span = false, want true")
	}
	if spanFilter(inSpan, otelsql.MethodConnExec, insertQuery, nil) {
		t.Error("spanFilter for InsertMarketSnapshot = true, want false")
	}
}
