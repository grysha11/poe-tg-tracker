package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/grysha11/poe-tg-tracker/internal/retry"
)

const (
	apiBase     = "https://api.telegram.org"
	maxRespBody = 8 << 20
)

var sendPolicy = retry.Policy{Attempts: 3, Base: 500 * time.Millisecond, Max: 5 * time.Second}

var (
	requestsTotal   metric.Int64Counter
	requestDuration metric.Float64Histogram
)

func init() {
	meter := otel.Meter("poetracker/telegram")
	var err error
	requestsTotal, err = meter.Int64Counter("poetracker.telegram.requests",
		metric.WithDescription("Telegram Bot API calls by method and HTTP status (\"error\" when no response)."))
	if err != nil {
		otel.Handle(err)
	}
	requestDuration, err = meter.Float64Histogram("poetracker.telegram.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Telegram Bot API call latency, including getUpdates long polls."),
		metric.WithExplicitBucketBoundaries(0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60))
	if err != nil {
		otel.Handle(err)
	}
}

type Bot struct {
	token string
	base  string
	http  *http.Client
}

func NewBot(token string) *Bot {
	return &Bot{
		token: token,
		base:  apiBase,
		http:  &http.Client{Timeout: 65 * time.Second},
	}
}

type Chat struct {
	ID int64 `json:"id"`
}

type User struct {
	ID int64 `json:"id"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	From      *User  `json:"from,omitempty"`
	Text      string `json:"text"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from,omitempty"`
	Data    string   `json:"data"`
	Message *Message `json:"message"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type APIError struct {
	Method      string
	Status      int
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Status, e.Description)
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (b *Bot) do(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("telegram %s: encode: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/bot"+b.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("telegram %s: build request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := b.http.Do(req)
	if err != nil {
		err = redact(method, err)
		b.observe(ctx, method, "error", start)
		slog.WarnContext(ctx, "telegram request failed", "method", method, "dur", time.Since(start), "err", err)
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBody))
	b.observe(ctx, method, strconv.Itoa(resp.StatusCode), start)
	if err != nil {
		return nil, fmt.Errorf("telegram %s: read body: %w", method, redact(method, err))
	}

	var out apiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("telegram %s: %d: decode: %w", method, resp.StatusCode, err)
	}
	if !out.OK || resp.StatusCode != http.StatusOK {
		apiErr := &APIError{Method: method, Status: resp.StatusCode, Description: out.Description}
		if out.Parameters != nil && out.Parameters.RetryAfter > 0 {
			apiErr.RetryAfter = time.Duration(out.Parameters.RetryAfter) * time.Second
		}
		slog.WarnContext(ctx, "telegram api error", "method", method, "status", resp.StatusCode, "description", out.Description, "retry_after", apiErr.RetryAfter)
		return nil, apiErr
	}

	slog.DebugContext(ctx, "telegram request", "method", method, "status", resp.StatusCode, "dur", time.Since(start))
	return out.Result, nil
}

func (b *Bot) observe(ctx context.Context, method, status string, start time.Time) {
	m := attribute.String("method", method)
	requestsTotal.Add(ctx, 1, metric.WithAttributes(m, attribute.String("status", status)))
	requestDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(m))
}

func (b *Bot) send(ctx context.Context, method string, payload any) error {
	return retry.Do(ctx, "telegram", sendPolicy, func(ctx context.Context) error {
		_, err := b.do(ctx, method, payload)
		return classifySend(err)
	})
}

func classifySend(err error) error {
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		switch {
		case apiErr.Status == http.StatusTooManyRequests:
			return retry.RetryableAfter(err, apiErr.RetryAfter)
		case apiErr.Status >= 500:
			return retry.Retryable(err)
		}
		return err
	}
	if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "dial" {
		return retry.Retryable(err)
	}
	return err
}

func redact(method string, err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}
	return fmt.Errorf("telegram %s: %w", method, err)
}

func (b *Bot) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	payload := map[string]any{
		"timeout":         timeoutSec,
		"allowed_updates": []string{"message", "callback_query"},
	}
	if offset > 0 {
		payload["offset"] = offset
	}

	raw, err := b.do(ctx, "getUpdates", payload)
	if err != nil {
		return nil, err
	}

	var updates []Update
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, fmt.Errorf("telegram getUpdates: decode updates: %w", err)
	}
	return updates, nil
}

func (b *Bot) SendMessage(ctx context.Context, chatID int64, text string, markup *InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	return b.send(ctx, "sendMessage", payload)
}

func (b *Bot) EditMessageText(ctx context.Context, chatID, messageID int64, text string, markup *InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}

	err := b.send(ctx, "editMessageText", payload)
	if apiErr, ok := errors.AsType[*APIError](err); ok && strings.Contains(apiErr.Description, "message is not modified") {
		return nil
	}
	return err
}

func (b *Bot) AnswerCallbackQuery(ctx context.Context, queryID, text string) error {
	payload := map[string]any{"callback_query_id": queryID}
	if text != "" {
		payload["text"] = text
	}
	return b.send(ctx, "answerCallbackQuery", payload)
}

func Command(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", false
	}
	cmd := strings.TrimPrefix(fields[0], "/")
	if i := strings.Index(cmd, "@"); i >= 0 {
		cmd = cmd[:i]
	}
	if cmd == "" {
		return "", false
	}
	return strings.ToLower(cmd), true
}
