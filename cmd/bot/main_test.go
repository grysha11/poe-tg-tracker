package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
  "lastFetchUtc": "1790362863",
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

	text, _, err := app.buildRates(context.Background(), priceView, allCategories)
	if err != nil {
		t.Fatalf("buildRates: %v", err)
	}

	if gotQuery.Get("view") != "RATE_VIEW_PRICE" || gotQuery.Get("league") != "Forbidden Rites" || gotQuery.Get("limit") != "10" {
		t.Errorf("gateway query = %v, want price view, configured league, limit 10", gotQuery)
	}
	if gotQuery.Has("categories") {
		t.Errorf("gateway query = %v, want no categories filter when all are selected", gotQuery)
	}

	for _, want := range []string{
		"💰 <b>Most expensive — Forbidden Rites</b>",
		"1 Mirror of Kalandra = <b>4000</b>",
		"range 2500–5000 · 119704 Divine Orb traded",
		"1 Divine Orb = <b>75.0</b>",
		"Orb of Annulment <i>(via Chaos Orb)</i>",
		"<i>range 70.0–80.0 Divine Orb</i>",
		"Last fetch time: 19:01 Sep 25 UTC",
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
	if _, _, err := app.buildRates(context.Background(), volumeView, allCategories); err == nil {
		t.Fatal("buildRates succeeded, want the gateway's 404 surfaced as an error")
	}
}

func TestRatesKeyboard_MarksActiveViewAndRoutesBack(t *testing.T) {
	kb := ratesKeyboard(priceView, allCategories, 0).InlineKeyboard

	want := []string{"📊 Top volume", "• 💰 Most expensive"}
	for i, b := range kb[0] {
		if b.Text != want[i] {
			t.Errorf("button %d text = %q, want %q", i, b.Text, want[i])
		}
		prefix, key, _ := strings.Cut(b.CallbackData, ":")
		if v, ok := viewByKey(key); prefix != ratesCallback || !ok || v.key != rateViews[i].key {
			t.Errorf("button %d callback %q does not route back to view %q", i, b.CallbackData, rateViews[i].key)
		}
	}
	if b := kb[1][0]; b.Text != "🗂 Categories" || b.CallbackData != "cats:price" {
		t.Errorf("categories button = %q -> %q, want 🗂 Categories -> cats:price", b.Text, b.CallbackData)
	}
}

func TestRatesKeyboard_CarriesPartialSelection(t *testing.T) {
	kb := ratesKeyboard(volumeView, selection{mask: 0b101}, 3).InlineKeyboard
	if got := kb[0][1].CallbackData; got != "rates:price:5" {
		t.Errorf("price callback = %q, want rates:price:5", got)
	}
	if b := kb[1][0]; b.Text != "🗂 Categories (2/3)" || b.CallbackData != "cats:volume:5" {
		t.Errorf("categories button = %q -> %q, want count 2/3 -> cats:volume:5", b.Text, b.CallbackData)
	}
}

func TestSelection(t *testing.T) {
	sel, ok := parseSelection("")
	if !ok || !sel.all {
		t.Fatalf("parseSelection(\"\") = %+v, want all", sel)
	}
	if _, ok := parseSelection("zz"); ok {
		t.Error("parseSelection(zz) succeeded, want failure")
	}

	sel = allCategories.toggle(1, 3)
	if sel.all || sel.String() != "5" || sel.count(3) != 2 {
		t.Errorf("all minus index 1 = %+v (%q), want mask 5 with 2 selected", sel, sel.String())
	}
	if back := sel.toggle(1, 3); !back.all {
		t.Errorf("toggling the last missing category back = %+v, want all", back)
	}
	if got := sel.names([]string{"currency", "runes", uncategorized}); !slices.Equal(got, []string{"currency", uncategorized}) {
		t.Errorf("names = %v, want [currency uncategorized]", got)
	}
	if got := (selection{mask: 0b111}).names([]string{"a", "b", "c"}); got != nil {
		t.Errorf("full mask names = %v, want nil (no filter)", got)
	}
}

func TestCategoriesKeyboard(t *testing.T) {
	cats := []string{"currency", "runes", uncategorized}
	kb := categoriesKeyboard(volumeView, allCategories, cats).InlineKeyboard

	if len(kb) != 4 || len(kb[0]) != 2 || len(kb[1]) != 1 {
		t.Fatalf("rows = %v, want 2 toggle rows (2+1), select-all row, view row", kb)
	}
	if b := kb[0][1]; b.Text != "✅ Runes" || b.CallbackData != "cats:volume:5" {
		t.Errorf("runes toggle = %q -> %q, want ✅ Runes -> cats:volume:5", b.Text, b.CallbackData)
	}
	if b := kb[1][0]; b.Text != "✅ Other" {
		t.Errorf("uncategorized toggle = %q, want ✅ Other", b.Text)
	}
	if b := kb[2][0]; b.Text != "Unselect all" || b.CallbackData != "cats:volume:0" {
		t.Errorf("select-all button = %q -> %q, want Unselect all -> cats:volume:0", b.Text, b.CallbackData)
	}

	none := categoriesKeyboard(volumeView, selection{}, cats).InlineKeyboard
	if b := none[0][0]; b.Text != "▫️ Currency" || b.CallbackData != "cats:volume:1" {
		t.Errorf("currency toggle from none = %q -> %q, want ▫️ Currency -> cats:volume:1", b.Text, b.CallbackData)
	}
	if b := none[2][0]; b.Text != "Select all" || b.CallbackData != "cats:volume" {
		t.Errorf("select-all button = %q -> %q, want Select all -> cats:volume", b.Text, b.CallbackData)
	}
}

func TestBuildRates_FiltersBySelectedCategories(t *testing.T) {
	var gotQuery url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/categories":
			w.Write([]byte(`{"categories":["currency","runes","uncategorized"]}`))
		case "/v1/rates":
			gotQuery = r.URL.Query()
			w.Write([]byte(priceRatesJSON))
		}
	}))
	t.Cleanup(ts.Close)

	app := &App{gw: gatewayclient.New(ts.URL), cfg: config.Config{League: "Forbidden Rites"}}
	text, markup, err := app.buildRates(context.Background(), priceView, selection{mask: 0b101})
	if err != nil {
		t.Fatalf("buildRates: %v", err)
	}
	if got := gotQuery["categories"]; !slices.Equal(got, []string{"currency", "uncategorized"}) {
		t.Errorf("categories query = %v, want [currency uncategorized]", got)
	}
	if !strings.Contains(text, "<i>Categories: Currency, Other</i>") {
		t.Errorf("message missing category line\n--- message ---\n%s", text)
	}
	if got := markup.InlineKeyboard[1][0].Text; got != "🗂 Categories (2/3)" {
		t.Errorf("categories button = %q, want 🗂 Categories (2/3)", got)
	}

	gotQuery = nil
	if _, _, err := app.buildRates(context.Background(), priceView, selection{}); !errors.Is(err, errNoCategories) {
		t.Errorf("empty selection err = %v, want errNoCategories", err)
	}
	if gotQuery != nil {
		t.Error("empty selection still queried rates")
	}
}
