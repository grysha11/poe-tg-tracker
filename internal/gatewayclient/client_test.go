package gatewayclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/grysha11/poe-tg-tracker/internal/gatewayclient"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

const ratesJSON = `{
  "base": {"itemPath": "Metadata/Items/Currency/CurrencyModValues", "name": "Divine Orb", "tradeId": "divine"},
  "rates": [
    {"currency": {"itemPath": "Metadata/Items/Currency/CurrencyDuplicate", "name": "Mirror of Kalandra", "tradeId": "mirror"},
     "vwap": 0.00024, "low": 0.0002, "high": 0.0003, "baseVolume": "119704", "quoteVolume": "29", "via": null},
    {"currency": {"itemPath": "Metadata/Items/Currency/CurrencyAnnulment", "name": "Orb of Annulment", "tradeId": "annul"},
     "vwap": 75, "low": 70, "high": 80, "baseVolume": "100", "quoteVolume": "50",
     "via": {"itemPath": "Metadata/Items/Currency/CurrencyRerollRare", "name": "Chaos Orb", "tradeId": "chaos"}}
  ],
  "hourUtc": "1790359200",
  "league": "Forbidden Rites",
  "someFutureField": true
}`

func newServer(t *testing.T, handler http.HandlerFunc) *gatewayclient.Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return gatewayclient.New(ts.URL + "/")
}

func TestGetRates(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	client := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		w.Write([]byte(ratesJSON))
	})

	resp, err := client.GetRates(context.Background(), "Forbidden Rites", pb.RateView_RATE_VIEW_PRICE, 10)
	if err != nil {
		t.Fatalf("GetRates: %v", err)
	}

	if gotPath != "/v1/rates" {
		t.Errorf("path = %q, want /v1/rates", gotPath)
	}
	if gotQuery.Get("league") != "Forbidden Rites" || gotQuery.Get("view") != "RATE_VIEW_PRICE" || gotQuery.Get("limit") != "10" {
		t.Errorf("query = %v, want league/view/limit set", gotQuery)
	}

	if resp.GetHourUtc() != 1790359200 {
		t.Errorf("hourUtc = %d, want 1790359200 (decoded from JSON string)", resp.GetHourUtc())
	}
	rates := resp.GetRates()
	if len(rates) != 2 {
		t.Fatalf("len(rates) = %d, want 2", len(rates))
	}
	if rates[0].GetBaseVolume() != 119704 || rates[0].GetVia() != nil {
		t.Errorf("rates[0] = %+v, want baseVolume 119704 and no via", rates[0])
	}
	if rates[1].GetVia().GetTradeId() != "chaos" {
		t.Errorf("rates[1].via = %v, want chaos", rates[1].GetVia())
	}
}

func TestGetRates_OmitsUnsetParams(t *testing.T) {
	var rawQuery string
	client := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Write([]byte(ratesJSON))
	})

	if _, err := client.GetRates(context.Background(), "", pb.RateView_RATE_VIEW_UNSPECIFIED, 0); err != nil {
		t.Fatalf("GetRates: %v", err)
	}
	if rawQuery != "" {
		t.Errorf("query = %q, want empty so the server applies its defaults", rawQuery)
	}
}

func TestGetRates_ErrorCarriesStatusAndMessage(t *testing.T) {
	client := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code":5,"message":"no snapshots yet for league \"Nope\"","details":[]}`))
	})

	_, err := client.GetRates(context.Background(), "Nope", pb.RateView_RATE_VIEW_VOLUME, 0)
	var apiErr *gatewayclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusNotFound || apiErr.Message != `no snapshots yet for league "Nope"` {
		t.Errorf("APIError = %+v, want 404 with upstream message", apiErr)
	}
}

func TestListLeagues(t *testing.T) {
	client := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/leagues" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"leagues":["Forbidden Rites (1802 markets)","Standard (321 markets)"]}`))
	})

	leagues, err := client.ListLeagues(context.Background())
	if err != nil {
		t.Fatalf("ListLeagues: %v", err)
	}
	if len(leagues) != 2 || leagues[0] != "Forbidden Rites (1802 markets)" {
		t.Errorf("leagues = %v", leagues)
	}
}
