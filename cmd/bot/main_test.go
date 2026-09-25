package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/gatewayclient"
)

const priceRatesJSON = `{
  "base": {"itemPath": "Metadata/Items/Currency/CurrencyModValues", "name": "Divine Orb", "tradeId": "divine"},
  "rates": [
    {"currency": {"itemPath": "Metadata/Items/Currency/CurrencyDuplicate", "name": "Mirror of Kalandra", "tradeId": "mirror"},
     "vwap": 0.00025, "low": 0.0002, "high": 0.0004, "baseVolume": "119704", "quoteVolume": "29", "via": null},
    {"currency": {"itemPath": "Metadata/Items/Currency/CurrencyAnnulment", "name": "Orb of Annulment", "tradeId": "annul"},
     "vwap": 75, "low": 70, "high": 80, "baseVolume": "100", "quoteVolume": "50",
     "via": {"itemPath": "Metadata/Items/Currency/CurrencyRerollRare", "name": "Chaos Orb", "tradeId": "chaos"}}
  ],
  "hourUtc": "1790359200",
  "league": "Forbidden Rites"
}`

func TestBuildRates_FormatsGatewayResponse(t *testing.T) {
	var gotQuery url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(priceRatesJSON))
	}))
	t.Cleanup(ts.Close)

	app := &App{
		gw:  gatewayclient.New(ts.URL),
		cfg: config.Config{League: "Forbidden Rites"},
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	text, err := app.buildRates(context.Background(), "price")
	if err != nil {
		t.Fatalf("buildRates: %v", err)
	}

	if gotQuery.Get("view") != "RATE_VIEW_PRICE" || gotQuery.Get("league") != "Forbidden Rites" || gotQuery.Get("limit") != "10" {
		t.Errorf("gateway query = %v, want price view, configured league, limit 10", gotQuery)
	}

	for _, want := range []string{
		"💰 <b>Most expensive — Forbidden Rites</b>",
		"1 Mirror of Kalandra = <b>4000</b>",
		"range 2500–5000 · 119704 Divine Orb traded",
		"1 Divine Orb = <b>75.0</b>",
		"Orb of Annulment <i>(via Chaos Orb)</i>",
		"<i>range 70.0–80.0 Divine Orb</i>",
		"Hour from 18:00 Sep 25 UTC",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("message missing %q\n--- message ---\n%s", want, text)
		}
	}
}

func TestBuildRates_GatewayErrorIsReturned(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code":5,"message":"no snapshots yet for league \"Forbidden Rites\""}`))
	}))
	t.Cleanup(ts.Close)

	app := &App{gw: gatewayclient.New(ts.URL), cfg: config.Config{League: "Forbidden Rites"}}
	if _, err := app.buildRates(context.Background(), "volume"); err == nil {
		t.Fatal("buildRates succeeded, want the gateway's 404 surfaced as an error")
	}
}
