package main

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/telegram"
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
		a.log.Warn("rejected message: user not whitelisted", "user_id", uid, "chat_id", msg.Chat.ID)
		a.send(ctx, msg.Chat.ID, "You're not authorized to use this bot.", nil)
		return
	}

	cmd, ok := telegram.Command(msg.Text)
	if !ok {
		return
	}

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
		a.send(ctx, msg.Chat.ID, text, ratesKeyboard(volumeView))

	case "rates":
		text, err := a.buildRates(ctx, volumeView)
		if err != nil {
			a.log.Error("rates fetch failed", "err", err)
			a.send(ctx, msg.Chat.ID, "Couldn't get rates right now. Try again shortly.", ratesKeyboard(volumeView))
			return
		}
		a.send(ctx, msg.Chat.ID, text, ratesKeyboard(volumeView))

	case "leagues":
		leagues, err := a.gw.ListLeagues(ctx)
		if err != nil {
			a.log.Error("leagues fetch failed", "err", err)
			a.send(ctx, msg.Chat.ID, "Couldn't reach the exchange API.", nil)
			return
		}
		if len(leagues) == 0 {
			a.send(ctx, msg.Chat.ID, "No leagues in that hour.", nil)
			return
		}
		a.send(ctx, msg.Chat.ID, "<b>Leagues seen</b>\n"+strings.Join(leagues, "\n"), nil)
	}
}

func (a *App) handleCallback(ctx context.Context, cb *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if cb.From == nil || !a.isWhitelisted(cb.From.ID) {
		var uid int64
		if cb.From != nil {
			uid = cb.From.ID
		}
		a.log.Warn("rejected callback: user not whitelisted", "user_id", uid)
		_ = a.bot.AnswerCallbackQuery(ctx, cb.ID, "Not authorized")
		return
	}

	prefix, key, _ := strings.Cut(cb.Data, ":")
	view, ok := viewByKey(key)
	if prefix != ratesCallback || !ok || cb.Message == nil {
		_ = a.bot.AnswerCallbackQuery(ctx, cb.ID, "")
		return
	}

	text, err := a.buildRates(ctx, view)
	if err != nil {
		a.log.Error("callback rates fetch failed", "err", err)
		_ = a.bot.AnswerCallbackQuery(ctx, cb.ID, "Couldn't load rates")
		return
	}

	if err := a.bot.AnswerCallbackQuery(ctx, cb.ID, "Updated"); err != nil {
		a.log.Error("answerCallbackQuery failed", "err", err)
	}

	if err := a.bot.EditMessageText(ctx, cb.Message.Chat.ID, cb.Message.MessageID, text, ratesKeyboard(view)); err != nil {
		a.log.Error("editMessageText failed", "err", err)
	}
}

func (a *App) send(ctx context.Context, chatID int64, text string, markup *telegram.InlineKeyboardMarkup) {
	if err := a.bot.SendMessage(ctx, chatID, text, markup); err != nil {
		a.log.Error("sendMessage failed", "err", err)
	}
}

func (a *App) buildRates(ctx context.Context, view rateView) (string, error) {
	resp, err := view.load(ctx, a)
	if err != nil {
		return "", err
	}

	return formatRanked(view, resp), nil
}
