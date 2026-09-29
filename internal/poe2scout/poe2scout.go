// Package poe2scout is a minimal client for the public api.poe2scout.com API,
// used to bulk-resolve currency item_path identifiers (e.g.
// "Metadata/Items/Currency/CurrencyRerollRare") to their real trade id and
// display name (e.g. "chaos", "Chaos Orb") and category (e.g. "currency")
// for currency curation.
package poe2scout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/retry"
	"github.com/grysha11/poe-tg-tracker/internal/telemetry"
)

const apiBase = "https://api.poe2scout.com"

var pagePolicy = retry.Policy{Attempts: 4, Base: 500 * time.Millisecond, Max: 8 * time.Second}

type Client struct {
	HTTP      *http.Client
	UserAgent string
}

func NewClient(userAgent string) *Client {
	return &Client{HTTP: telemetry.HTTPClient("poe2scout", 30*time.Second), UserAgent: userAgent}
}

type CurrencyItem struct {
	ApiId          string  `json:"ApiId"`
	BaseItemTypeId *string `json:"BaseItemTypeId"`
	Text           string  `json:"Text"`
	CategoryApiId  string  `json:"CategoryApiId"`
}

type categoriesResponse struct {
	CurrencyCategories []struct {
		ApiId string `json:"ApiId"`
	} `json:"CurrencyCategories"`
}

type byCategoryResponse struct {
	Pages int            `json:"Pages"`
	Items []CurrencyItem `json:"Items"`
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return retry.Do(ctx, "poe2scout", pagePolicy, func(ctx context.Context) error {
		return c.getOnce(ctx, path, out)
	})
}

func (c *Client) getOnce(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
	if err != nil {
		return err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return retry.Retryable(err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		err := fmt.Errorf("poe2scout %d: %s", resp.StatusCode, string(body))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return retry.RetryableAfter(err, retry.AfterHeader(resp.Header))
		}
		return err
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return retry.Retryable(fmt.Errorf("poe2scout decode: %w", err))
	}
	return nil
}

func (c *Client) currencyCategories(ctx context.Context, realm, league string) ([]string, error) {
	var out categoriesResponse
	path := fmt.Sprintf("/%s/Leagues/%s/Items/Categories", url.PathEscape(realm), url.PathEscape(league))
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(out.CurrencyCategories))
	for _, cat := range out.CurrencyCategories {
		ids = append(ids, cat.ApiId)
	}
	return ids, nil
}

func (c *Client) AllCurrencyItems(ctx context.Context, realm, league string) ([]CurrencyItem, error) {
	categories, err := c.currencyCategories(ctx, realm, league)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}

	slog.DebugContext(ctx, "poe2scout categories", "realm", realm, "league", league, "count", len(categories))

	start := time.Now()
	var all []CurrencyItem
	for _, category := range categories {
		for page := 1; ; page++ {
			var out byCategoryResponse
			path := fmt.Sprintf("/%s/Leagues/%s/Currencies/ByCategory?category=%s&perPage=100&page=%d&dataPoints=7",
				url.PathEscape(realm), url.PathEscape(league), url.QueryEscape(category), page)
			if err := c.get(ctx, path, &out); err != nil {
				return nil, fmt.Errorf("category %q page %d: %w", category, page, err)
			}

			all = append(all, out.Items...)
			slog.DebugContext(ctx, "poe2scout page", "category", category, "page", page, "pages", out.Pages, "items", len(out.Items))
			if page >= out.Pages || out.Pages == 0 {
				break
			}
		}
	}
	slog.InfoContext(ctx, "poe2scout currencies fetched", "realm", realm, "league", league, "categories", len(categories), "items", len(all), "dur", time.Since(start))
	return all, nil
}
