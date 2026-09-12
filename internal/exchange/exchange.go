package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const apiBase = "https://web.poecdn.com/api/currency-exchange/poe2"

func NewClient(userAgent string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: userAgent,
	}
}

func (c *Client) Fetch(ctx context.Context, ts int64) (*Digest, error) {
	url := fmt.Sprintf("%s/%d", apiBase, ts)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &Digest{}, fmt.Errorf("exchange %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	var d Digest
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("decode digest: %d", err)
	}

	return &d, nil
}

func AlignHour(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}

func findMarket(markets []Market, league, base, quote string) (Market, bool) {
	for _, m := range markets {
		if m.League != league || len(m.MarketPair) != 2 {
			continue
		}
		a, b := m.MarketPair[0], m.MarketPair[1]
		if (a == base && b == quote) || (a == quote && b == base) {
			return m, true
		}
	}
	return Market{}, false
}

func rateFrom(m Market, base, quote string) (Rate, bool) {
	baseVol := m.VolumeTraded[base]
	quoteVol := m.VolumeTraded[quote]
	if baseVol == 0 || quoteVol == 0 {
		return Rate{}, false
	}

	r := Rate{
		Quote:     quote,
		VWAP:      float64(quoteVol) / float64(baseVol),
		DivineVol: baseVol,
		QuoteVol:  quoteVol,
	}

	var bounds []float64
	for _, side := range []map[string]uint64{m.LowestRatio, m.HighestRatio} {
		if side[base] == 0 {
			continue
		}
		bounds = append(bounds, float64(side[quote])/float64(side[base]))
	}
	sort.Float64s(bounds)

	switch len(bounds) {
	case 0:
		r.Low, r.High = r.VWAP, r.VWAP
	case 1:
		r.Low, r.High = bounds[0], bounds[0]
	default:
		r.Low, r.High = bounds[0], bounds[len(bounds)-1]
	}
	return r, true
}

func Leagues(d *Digest) []string {
	counts := map[string]int{}
	for _, m := range d.Markets {
		counts[m.League]++
	}
	out := make([]string, 0, len(counts))
	for lg := range counts {
		out = append(out, lg)
	}
	sort.Slice(out, func(i, j int) bool { return counts[out[i]] > counts[out[j]] })
	for i, lg := range out {
		out[i] = fmt.Sprintf("%s (%d markets)", lg, counts[lg])
	}
	return out
}

func snapshotFrom(d *Digest, league, base string, quotes []Currency, hour time.Time) (*Snapshot, bool) {
	rates := make(map[string]Rate, len(quotes))
	for _, quote := range quotes {
		m, ok := findMarket(d.Markets, league, base, quote.ID)
		if !ok {
			continue
		}
		r, ok := rateFrom(m, base, quote.ID)
		if !ok {
			continue
		}
		rates[quote.ID] = r
	}
	if len(rates) == 0 {
		return nil, false
	}
	return &Snapshot{League: league, HourUTC: hour, Base: base, Rates: rates}, true
}

func (c *Client) LastHour(ctx context.Context, league string) (*Snapshot, error) {
	newest := AlignHour(time.Now()).Add(-time.Hour)

	var firstErr error
	for _, hour := range []time.Time{newest, newest.Add(-time.Hour)} {
		d, err := c.Fetch(ctx, hour.Unix())
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if snap, ok := snapshotFrom(d, league, Divine.ID, Quotes, hour); ok {
			return snap, nil
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("no Divine markets for league %q in the last settled hour", league)
}

func NewCache(c *Client, league string, ttl time.Duration) *Cache {
	return &Cache{client: c, league: league, ttl: ttl}
}

func (c *Cache) Get(ctx context.Context) (*Snapshot, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.snap != nil && time.Since(c.fetched) < c.ttl {
		return c.snap, false, nil
	}

	snap, err := c.client.LastHour(ctx, c.league)
	if err != nil {
		if c.snap != nil {
			return c.snap, false, nil
		}
		return nil, false, err
	}

	c.snap = snap
	c.fetched = time.Now()
	return snap, true, nil
}
