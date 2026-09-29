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
	"github.com/grysha11/poe-tg-tracker/internal/retry"
	"github.com/grysha11/poe-tg-tracker/internal/telemetry"
)

const maxBody = 4 << 20

var getPolicy = retry.Policy{Attempts: 3, Base: 200 * time.Millisecond, Max: 2 * time.Second}

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		http: telemetry.HTTPClient("gateway", 30*time.Second),
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

func (c *Client) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/readyz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gateway /readyz: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode, Message: "not ready"}
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out proto.Message) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var body []byte
	err := retry.Do(ctx, "gateway", getPolicy, func(ctx context.Context) error {
		var err error
		body, err = c.fetch(ctx, path, u)
		return err
	})
	if err != nil {
		return err
	}

	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, out); err != nil {
		return fmt.Errorf("gateway %s: decode: %w", path, err)
	}
	return nil
}

func (c *Client) fetch(ctx context.Context, path, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, retry.Retryable(fmt.Errorf("gateway %s: %w", path, err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, retry.Retryable(fmt.Errorf("gateway %s: read body: %w", path, err))
	}

	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &e) != nil || e.Message == "" {
			e.Message = strings.TrimSpace(string(body))
		}
		apiErr := &APIError{Status: resp.StatusCode, Message: e.Message}
		switch resp.StatusCode {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return nil, retry.Retryable(apiErr)
		}
		return nil, apiErr
	}
	return body, nil
}
