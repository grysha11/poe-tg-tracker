package exchange

import "net/http"

type Currency struct {
	ID      string
	Name    string
	TradeID string // key into internal/emoji's ids.json
}

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
	Quote    string
	VWAP     float64
	Low      float64
	High     float64
	BaseVol  uint64
	QuoteVol uint64
}

type SnapshotRow struct {
	ItemA, ItemB                 Currency
	VolumeA, VolumeB             uint64
	LowestRatioA, LowestRatioB   uint64
	HighestRatioA, HighestRatioB uint64
}

type CurrencyRate struct {
	Currency Currency
	Rate     Rate
	Via      *Currency
}

type Client struct {
	HTTP      *http.Client
	UserAgent string
}
