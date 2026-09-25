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
	"net/http"
	"net/url"
	"time"
)

const apiBase = "https://api.poe2scout.com"

type Client struct {
	HTTP      *http.Client
	UserAgent string
}

func NewClient(userAgent string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, UserAgent: userAgent}
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

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
	if err != nil {
		return err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("poe2scout %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(out)
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
			if page >= out.Pages || out.Pages == 0 {
				break
			}
		}
	}
	return all, nil
}
