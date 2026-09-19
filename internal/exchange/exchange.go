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

func (c *Client) FetchRaw(ctx context.Context, ts int64) ([]byte, error) {
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
		return nil, fmt.Errorf("exchange %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	return io.ReadAll(resp.Body)
}

func (c *Client) Fetch(ctx context.Context, ts int64) (*Digest, error) {
	raw, err := c.FetchRaw(ctx, ts)
	if err != nil {
		return nil, err
	}

	var d Digest
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("decode digest: %w", err)
	}

	return &d, nil
}

func AlignHour(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}

func computeRate(quoteID string, baseVol, quoteVol, baseLow, quoteLow, baseHigh, quoteHigh uint64) (Rate, bool) {
	if baseVol == 0 || quoteVol == 0 {
		return Rate{}, false
	}

	r := Rate{
		Quote:    quoteID,
		VWAP:     float64(quoteVol) / float64(baseVol),
		BaseVol:  baseVol,
		QuoteVol: quoteVol,
	}

	var bounds []float64
	if baseLow != 0 {
		bounds = append(bounds, float64(quoteLow)/float64(baseLow))
	}
	if baseHigh != 0 {
		bounds = append(bounds, float64(quoteHigh)/float64(baseHigh))
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
