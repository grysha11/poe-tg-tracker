package exchange

import (
	"net/http"
	"sync"
	"time"
)

type Currency struct {
	ID   string
	Name string
}

var (
	Divine = Currency{ID: "Metadata/Items/Currency/CurrencyModValues", Name: "Divine"}
	Chaos  = Currency{ID: "Metadata/Items/Currency/CurrencyRerollRare", Name: "Chaos"}
	Exalt  = Currency{ID: "Metadata/Items/Currency/CurrencyAddModToRare", Name: "Exalt"}
)

var Quotes = []Currency{Chaos, Exalt}

type Digest struct {
	NextChangeID int64    `json:"next_change_id"`
	Markets      []Market `json:"markets"`
}

type Market struct {
	League       string            `json:"league"`
	MarketID     string            `json:"market_id"`
	MarketPair   []string          `json:"market_pair"`
	VolumeTraded map[string]uint64 `json:"volume_traded"`
	LowestRatio  map[string]uint64 `json:"lowest_ratio"`
	HighestRatio map[string]uint64 `json:"highest_ratio"`
}

type Rate struct {
	Quote     string
	VWAP      float64
	Low       float64
	High      float64
	DivineVol uint64
	QuoteVol  uint64
}

type Snapshot struct {
	League  string
	HourUTC time.Time
	Base    string // currency all rates are quoted against, e.g. Divine.ID
	Rates   map[string]Rate
}

func (s *Snapshot) Thin(min uint64) bool {
	for _, r := range s.Rates {
		if r.DivineVol < min {
			return true
		}
	}
	return false
}

type Client struct {
	HTTP      *http.Client
	UserAgent string
}

type Cache struct {
	client *Client
	league string
	ttl    time.Duration

	mu      sync.Mutex
	snap    *Snapshot
	fetched time.Time
}
