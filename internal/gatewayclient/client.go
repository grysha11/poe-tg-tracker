package gatewayclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

const maxBody = 4 << 20

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gateway: %d %s", e.Status, e.Message)
}

func (c *Client) GetRates(ctx context.Context, league string, view pb.RateView, limit int32) (*pb.GetRatesResponse, error) {
	q := url.Values{}
	if league != "" {
		q.Set("league", league)
	}
	if view != pb.RateView_RATE_VIEW_UNSPECIFIED {
		q.Set("view", view.String())
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(int(limit)))
	}

	var out pb.GetRatesResponse
	if err := c.get(ctx, "/v1/rates", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListLeagues(ctx context.Context) ([]string, error) {
	var out pb.ListLeaguesResponse
	if err := c.get(ctx, "/v1/leagues", nil, &out); err != nil {
		return nil, err
	}
	return out.GetLeagues(), nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out proto.Message) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gateway %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("gateway %s: read body: %w", path, err)
	}

	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &e) != nil || e.Message == "" {
			e.Message = strings.TrimSpace(string(body))
		}
		return &APIError{Status: resp.StatusCode, Message: e.Message}
	}

	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, out); err != nil {
		return fmt.Errorf("gateway %s: decode: %w", path, err)
	}
	return nil
}
