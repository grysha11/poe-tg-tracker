package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)


type Bot struct {
	token string
	http  *http.Client
}

func NewBot(token string) *Bot {
	return &Bot{
		token: token,
		http: &http.Client{Timeout: 65 * time.Second},
	}
}

type Chat struct {
	ID int64 `json:"id"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	Data    string   `json:"data"`
	Message *Message `json:"message"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type updatesResponse struct {
	OK          bool     `json:"ok"`
	Result      []Update `json:"result"`
	Description string   `json:"description"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

func RatesKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{{
			{Text: "🔄 Last hour", CallbackData: "rates"},
		}},
	}
}

func (b *Bot) api(method string) string {
	return fmt.Sprintf("https://api.telegram.org/bot%s/%s", b.token, method)
}

func (b *Bot) post(ctx context.Context, method string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.api(method), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return respBody, fmt.Errorf("%s %d: %s", method, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func (b *Bot) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	q := url.Values{}
	q.Set("timeout", fmt.Sprint(timeoutSec))
	q.Set("allowed_updates", `["message","callback_query"]`)
	if offset > 0 {
		q.Set("offset", fmt.Sprint(offset))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.api("getUpdates")+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out updatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode updates: %w", err)
	}
	if !out.OK {
		return nil, fmt.Errorf("getUpdates: %s", out.Description)
	}
	return out.Result, nil
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
	_, err := b.post(ctx, "sendMessage", payload)
	return err
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

	body, err := b.post(ctx, "editMessageText", payload)
	if err != nil {
		if bytes.Contains(body, []byte("message is not modified")) {
			return nil
		}
		return err
	}
	return nil
}

func (b *Bot) AnswerCallbackQuery(ctx context.Context, queryID, text string) error {
	payload := map[string]any{"callback_query_id": queryID}
	if text != "" {
		payload["text"] = text
	}
	_, err := b.post(ctx, "answerCallbackQuery", payload)
	return err
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