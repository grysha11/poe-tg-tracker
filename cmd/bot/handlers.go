package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/grysha11/poe-tg-tracker/internal/telegram"
)

const (
	outcomeOK      = "ok"
	outcomeError   = "error"
	outcomeUnknown = "unknown"
	// outcomeEmptySelection is a view pressed with every category unselected.
	outcomeEmptySelection = "empty_selection"
	outcomeRejected       = "rejected"
)

var tracer = otel.Tracer("poetracker/bot")

func finishSpan(span trace.Span, outcome string) {
	span.SetAttributes(attribute.String("bot.outcome", outcome))
	if outcome == outcomeError {
		span.SetStatus(codes.Error, "handler failed")
	}
	span.End()
}

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
		ctx, span := tracer.Start(ctx, "bot.message", trace.WithAttributes(
			attribute.Int64("telegram.user_id", uid), attribute.Int64("telegram.chat_id", msg.Chat.ID)))
		defer finishSpan(span, outcomeRejected)

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

	ctx, span := tracer.Start(ctx, "bot.message", trace.WithAttributes(
		attribute.Int64("telegram.user_id", msg.From.ID), attribute.Int64("telegram.chat_id", msg.Chat.ID)))

	start := time.Now()
	a.log.InfoContext(ctx, "command received", "command", cmd, "user_id", msg.From.ID, "chat_id", msg.Chat.ID)

	outcome := a.runCommand(ctx, msg.Chat.ID, cmd)

	label := cmd
	if outcome == outcomeUnknown {
		label = outcomeUnknown
	}
	span.SetAttributes(attribute.String("bot.command", label))
	defer finishSpan(span, outcome)

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
			"Use 🗂 Categories to pick which item categories are ranked.",
			"",
			"/rates — show rates",
			"/leagues — list league strings",
		}, "\n")
		return a.send(ctx, chatID, text, ratesKeyboard(volumeView, allCategories, 0))

	case "rates":
		text, markup, err := a.buildRates(ctx, volumeView, allCategories)
		if err != nil {
			a.log.ErrorContext(ctx, "rates fetch failed", "chat_id", chatID, "err", err)
			a.send(ctx, chatID, "Couldn't get rates right now. Try again shortly.", ratesKeyboard(volumeView, allCategories, 0))
			return outcomeError
		}
		return a.send(ctx, chatID, text, markup)

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
		ctx, span := tracer.Start(ctx, "bot.callback", trace.WithAttributes(attribute.Int64("telegram.user_id", uid)))
		defer finishSpan(span, outcomeRejected)

		rejectedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", "callback")))
		a.log.WarnContext(ctx, "rejected callback: user not whitelisted", "user_id", uid)
		if err := a.bot.AnswerCallbackQuery(ctx, cb.ID, "Not authorized"); err != nil {
			a.log.ErrorContext(ctx, "answerCallbackQuery failed", "user_id", uid, "err", err)
		}
		return
	}

	ctx, span := tracer.Start(ctx, "bot.callback", trace.WithAttributes(attribute.Int64("telegram.user_id", cb.From.ID)))

	start := time.Now()
	outcome, viewKey, sel := a.runCallback(ctx, cb)

	span.SetAttributes(attribute.String("bot.view", viewKey), attribute.Bool("bot.filtered", !sel.all))
	defer finishSpan(span, outcome)

	callbacksTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("view", viewKey), attribute.String("outcome", outcome), attribute.Bool("filtered", !sel.all)))
	args := []any{"data", cb.Data, "user_id", cb.From.ID, "outcome", outcome, "dur", time.Since(start)}
	if !sel.all {
		args = append(args, "categories", sel.names(a.cats))
	}
	a.log.InfoContext(ctx, "callback handled", args...)
}

func (a *App) runCallback(ctx context.Context, cb *telegram.CallbackQuery) (outcome, viewKey string, sel selection) {
	prefix, rest, _ := strings.Cut(cb.Data, ":")
	key, mask, _ := strings.Cut(rest, ":")
	view, okView := viewByKey(key)
	sel, okSel := parseSelection(mask)
	if (prefix != ratesCallback && prefix != categoriesCallback) || !okView || !okSel || cb.Message == nil {
		a.log.WarnContext(ctx, "unknown callback", "data", cb.Data, "user_id", cb.From.ID, "has_message", cb.Message != nil)
		a.answer(ctx, cb, "")
		return outcomeUnknown, outcomeUnknown, allCategories
	}

	chatID := cb.Message.Chat.ID
	var text string
	var markup *telegram.InlineKeyboardMarkup
	var err error
	toast := ""
	if prefix == categoriesCallback {
		viewKey = categoriesViewKey
		text, markup, err = a.buildCategories(ctx, view, sel)
	} else {
		viewKey, toast = view.key, "Updated"
		text, markup, err = a.buildRates(ctx, view, sel)
		if errors.Is(err, errNoCategories) {
			a.answer(ctx, cb, "Select at least one category")
			return outcomeEmptySelection, viewKey, sel
		}
	}
	if err != nil {
		a.log.ErrorContext(ctx, "callback load failed", "data", cb.Data, "chat_id", chatID, "err", err)
		a.answer(ctx, cb, "Couldn't load data")
		return outcomeError, viewKey, sel
	}

	outcome = outcomeOK
	if !a.answer(ctx, cb, toast) {
		outcome = outcomeError
	}
	if err := a.bot.EditMessageText(ctx, chatID, cb.Message.MessageID, text, markup); err != nil {
		a.log.ErrorContext(ctx, "editMessageText failed", "chat_id", chatID, "message_id", cb.Message.MessageID, "err", err)
		outcome = outcomeError
	}
	return outcome, viewKey, sel
}

func (a *App) answer(ctx context.Context, cb *telegram.CallbackQuery, text string) bool {
	if err := a.bot.AnswerCallbackQuery(ctx, cb.ID, text); err != nil {
		a.log.ErrorContext(ctx, "answerCallbackQuery failed", "user_id", cb.From.ID, "err", err)
		return false
	}
	return true
}

func (a *App) send(ctx context.Context, chatID int64, text string, markup *telegram.InlineKeyboardMarkup) string {
	if err := a.bot.SendMessage(ctx, chatID, text, markup); err != nil {
		a.log.ErrorContext(ctx, "sendMessage failed", "chat_id", chatID, "err", err)
		return outcomeError
	}
	return outcomeOK
}

var errNoCategories = errors.New("no categories selected")

func (a *App) buildRates(ctx context.Context, view rateView, sel selection) (string, *telegram.InlineKeyboardMarkup, error) {
	var cats, names []string
	if !sel.all {
		var err error
		if cats, err = a.categories(ctx); err != nil {
			return "", nil, err
		}
		if sel.count(len(cats)) == 0 {
			return "", nil, errNoCategories
		}
		names = sel.names(cats)
	}

	resp, err := a.gw.GetRates(ctx, a.cfg.League, view.rank, ratesLimit, names)
	if err != nil {
		return "", nil, err
	}
	return formatRanked(view, resp, names), ratesKeyboard(view, sel, len(cats)), nil
}

func (a *App) buildCategories(ctx context.Context, view rateView, sel selection) (string, *telegram.InlineKeyboardMarkup, error) {
	cats, err := a.categories(ctx)
	if err != nil {
		return "", nil, err
	}
	text := fmt.Sprintf("🗂 <b>Categories</b>\n\n%d of %d selected. Toggle categories, then pick a view.", sel.count(len(cats)), len(cats))
	return text, categoriesKeyboard(view, sel, cats), nil
}
