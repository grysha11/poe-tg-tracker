package exchange

import (
	"context"
	"net/http"
	"time"
)

const (
	DivineID = "Metadata/Items/Currency/CurrencyModValues"
	ChaosID  = "Metadata/Items/Currency/CurrencyRerollRare"
	ExaltID  = "Metadata/Items/Currency/CurrencyAddModToRare" 
)

const apiBase = "https://web.poecdn.com/api/currency-exchange/poe2"

type Digest struct {
	NextChangeID	int64		`json:"next_change_id"`
	Markets			[]Market	`json:"markets"`
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
	Quote      string  // base item id of the non-Divine side
	VWAP       float64 // volume-weighted average: the number you should display
	Low        float64 // cheapest observed, per Divine
	High       float64 // dearest observed, per Divine
	DivineVol  uint64
	QuoteVol   uint64
}

type Snapshot struct {
	League	string
	HourUTC	time.Time
	Chaos	Rate
	Exalt	Rate
}

type Client struct {
	HTTP		*http.Client
	UserAgent	string
}

func NewClient(userAgent string) *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 30 * time.Second},
		UserAgent: userAgent,
	}
}

// func (c *Client) Fetch(ctx context.Context, ts int64) (*Digest, error) {

// }
