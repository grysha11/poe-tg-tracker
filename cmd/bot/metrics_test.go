package main

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestPrimeMetrics_CreatesZeroSeries(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	primeMetrics(context.Background())

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	sums := map[string]metricdata.Sum[int64]{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if s, ok := m.Data.(metricdata.Sum[int64]); ok {
				sums[m.Name] = s
			}
		}
	}

	commands, ok := sums["poetracker.bot.commands"]
	if !ok {
		t.Fatalf("no poetracker.bot.commands series, got %v", keys(sums))
	}
	if len(commands.DataPoints) != 9 {
		t.Errorf("commands series = %d, want 9 (4 commands x ok/error + unknown)", len(commands.DataPoints))
	}
	ratesOK := false
	for _, dp := range commands.DataPoints {
		if dp.Value != 0 {
			t.Errorf("primed series %v = %d, want 0", dp.Attributes.ToSlice(), dp.Value)
		}
		cmd, _ := dp.Attributes.Value(attribute.Key("command"))
		outcome, _ := dp.Attributes.Value(attribute.Key("outcome"))
		if cmd.AsString() == "rates" && outcome.AsString() == outcomeOK {
			ratesOK = true
		}
	}
	if !ratesOK {
		t.Error("no rates/ok series")
	}

	if rejected := sums["poetracker.bot.rejected"]; len(rejected.DataPoints) != 2 {
		t.Errorf("rejected series = %d, want 2 (message, callback)", len(rejected.DataPoints))
	}
	if errs := sums["poetracker.bot.getupdates.errors"]; len(errs.DataPoints) != 1 {
		t.Errorf("getupdates errors series = %d, want 1", len(errs.DataPoints))
	}
}

func keys(m map[string]metricdata.Sum[int64]) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
