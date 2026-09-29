package main

import (
	"context"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/grysha11/poe-tg-tracker/internal/telegram"
)

const (
	outcomeOK      = "ok"
	outcomeError   = "error"
	outcomeUnknown = "unknown"
)

func (a *App) isWhitelisted(userID int64) bool {
	return slices.Contains(a.cfg.Whitelist, userID)
}

func (a *App) handleMessage(ctx context.Context, msg *telegram.Message) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if msg.From == nil || !a.isWhitelisted(msg.From.ID) {
		var uid int64
		if msg.From != nil {
			uid = msg.From.ID
		}
		rejectedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", "message")))
		a.log.WarnContext(ctx, "rejected message: user not whitelisted", "user_id", uid, "chat_id", msg.Chat.ID)
		a.send(ctx, msg.Chat.ID, "You're not authorized to use this bot.", nil)
		return
	}

	cmd, ok := telegram.Command(msg.Text)
	if !ok {
		a.log.DebugContext(ctx, "ignoring non-command message", "user_id", msg.From.ID, "chat_id", msg.Chat.ID)
		return
	}

	start := time.Now()
	a.log.InfoContext(ctx, "command received", "command", cmd, "user_id", msg.From.ID, "chat_id", msg.Chat.ID)

	outcome := a.runCommand(ctx, msg.Chat.ID, cmd)

	label := cmd
	if outcome == outcomeUnknown {
		label = outcomeUnknown
	}
	commandsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("command", label), attribute.String("outcome", outcome)))
	a.log.InfoContext(ctx, "command handled", "command", cmd, "user_id", msg.From.ID, "chat_id", msg.Chat.ID, "outcome", outcome, "dur", time.Since(start))
}

func (a *App) runCommand(ctx context.Context, chatID int64, cmd string) string {
	switch cmd {
	case "start", "help":
		text := strings.Join([]string{
			"<b>PoE2 rate tracker</b>",
			"",
			"Tap a button for the top Currency rates (priced in Divine) from the latest fetched hour.",
			"",
			"/rates — show rates",
			"/leagues — list league strings",
		}, "\n")
		return a.send(ctx, chatID, text, ratesKeyboard(volumeView))

	case "rates":
		text, err := a.buildRates(ctx, volumeView)
		if err != nil {
			a.log.ErrorContext(ctx, "rates fetch failed", "chat_id", chatID, "err", err)
			a.send(ctx, chatID, "Couldn't get rates right now. Try again shortly.", ratesKeyboard(volumeView))
			return outcomeError
		}
		return a.send(ctx, chatID, text, ratesKeyboard(volumeView))

	case "leagues":
		leagues, err := a.gw.ListLeagues(ctx)
		if err != nil {
			a.log.ErrorContext(ctx, "leagues fetch failed", "chat_id", chatID, "err", err)
			a.send(ctx, chatID, "Couldn't reach the exchange API.", nil)
			return outcomeError
		}
		if len(leagues) == 0 {
			return a.send(ctx, chatID, "No leagues in that hour.", nil)
		}
		return a.send(ctx, chatID, "<b>Leagues seen</b>\n"+strings.Join(leagues, "\n"), nil)
	}
	return outcomeUnknown
}

func (a *App) handleCallback(ctx context.Context, cb *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if cb.From == nil || !a.isWhitelisted(cb.From.ID) {
		var uid int64
		if cb.From != nil {
			uid = cb.From.ID
		}
		rejectedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", "callback")))
		a.log.WarnContext(ctx, "rejected callback: user not whitelisted", "user_id", uid)
		if err := a.bot.AnswerCallbackQuery(ctx, cb.ID, "Not authorized"); err != nil {
			a.log.ErrorContext(ctx, "answerCallbackQuery failed", "user_id", uid, "err", err)
		}
		return
	}

	start := time.Now()
	outcome, viewKey := a.runCallback(ctx, cb)

	callbacksTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("view", viewKey), attribute.String("outcome", outcome)))
	a.log.InfoContext(ctx, "callback handled", "data", cb.Data, "user_id", cb.From.ID, "outcome", outcome, "dur", time.Since(start))
}

func (a *App) runCallback(ctx context.Context, cb *telegram.CallbackQuery) (outcome, viewKey string) {
	prefix, key, _ := strings.Cut(cb.Data, ":")
	view, ok := viewByKey(key)
	if prefix != ratesCallback || !ok || cb.Message == nil {
		a.log.WarnContext(ctx, "unknown callback", "data", cb.Data, "user_id", cb.From.ID, "has_message", cb.Message != nil)
		if err := a.bot.AnswerCallbackQuery(ctx, cb.ID, ""); err != nil {
			a.log.ErrorContext(ctx, "answerCallbackQuery failed", "user_id", cb.From.ID, "err", err)
		}
		return outcomeUnknown, outcomeUnknown
	}

	chatID := cb.Message.Chat.ID
	text, err := a.buildRates(ctx, view)
	if err != nil {
		a.log.ErrorContext(ctx, "callback rates fetch failed", "view", view.key, "chat_id", chatID, "err", err)
		if err := a.bot.AnswerCallbackQuery(ctx, cb.ID, "Couldn't load rates"); err != nil {
			a.log.ErrorContext(ctx, "answerCallbackQuery failed", "chat_id", chatID, "err", err)
		}
		return outcomeError, view.key
	}

	outcome = outcomeOK
	if err := a.bot.AnswerCallbackQuery(ctx, cb.ID, "Updated"); err != nil {
		a.log.ErrorContext(ctx, "answerCallbackQuery failed", "chat_id", chatID, "err", err)
		outcome = outcomeError
	}

	if err := a.bot.EditMessageText(ctx, chatID, cb.Message.MessageID, text, ratesKeyboard(view)); err != nil {
		a.log.ErrorContext(ctx, "editMessageText failed", "chat_id", chatID, "message_id", cb.Message.MessageID, "err", err)
		outcome = outcomeError
	}
	return outcome, view.key
}

func (a *App) send(ctx context.Context, chatID int64, text string, markup *telegram.InlineKeyboardMarkup) string {
	if err := a.bot.SendMessage(ctx, chatID, text, markup); err != nil {
		a.log.ErrorContext(ctx, "sendMessage failed", "chat_id", chatID, "err", err)
		return outcomeError
	}
	return outcomeOK
}

func (a *App) buildRates(ctx context.Context, view rateView) (string, error) {
	resp, err := view.load(ctx, a)
	if err != nil {
		return "", err
	}

	return formatRanked(view, resp), nil
}
