package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/emoji"
	"github.com/grysha11/poe-tg-tracker/internal/gatewayclient"
	"github.com/grysha11/poe-tg-tracker/internal/logger"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
	"github.com/grysha11/poe-tg-tracker/internal/telegram"
)

type App struct {
	bot *telegram.Bot
	gw  *gatewayclient.Client
	cfg config.Config
	log *slog.Logger
}

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := &App{
		bot: telegram.NewBot(cfg.TelegramToken),
		gw:  gatewayclient.New(cfg.GatewayAddr),
		cfg: cfg,
		log: log,
	}

	log.Info("starting", "league", cfg.League, "gateway", cfg.GatewayAddr)

	var offset int64
	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return
		default:
		}

		updates, err := app.bot.GetUpdates(ctx, offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("getUpdates failed", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1

			switch {
			case u.CallbackQuery != nil:
				app.handleCallback(ctx, u.CallbackQuery)
			case u.Message != nil:
				app.handleMessage(ctx, u.Message)
			}
		}
	}
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

func formatValue(v float64) string {
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

func formatRanked(view rateView, resp *pb.GetRatesResponse) string {
	var b strings.Builder
	base := resp.GetBase()

	fmt.Fprintf(&b, "%s <b>%s — %s</b>\n\n", view.icon, view.title, resp.GetLeague())

	if len(resp.GetRates()) == 0 {
		b.WriteString("No data for this hour yet.\n\n")
	}

	for _, r := range resp.GetRates() {
		via := ""
		if r.GetVia() != nil {
			via = fmt.Sprintf(" <i>(via %s)</i>", r.GetVia().GetName())
		}

		value, low, high := r.GetVwap(), r.GetLow(), r.GetHigh()
		left, right := base, r.GetCurrency()
		if value < 1 {
			invLow, invHigh := low, high
			if high > 0 {
				invLow = 1 / high
			}
			if low > 0 {
				invHigh = 1 / low
			}
			value, low, high = 1/value, invLow, invHigh
			left, right = right, left
		}

		fmt.Fprintf(&b, "%s 1 %s = <b>%s</b> %s %s%s\n",
			emoji.Tag(left.GetTradeId()), left.GetName(), formatValue(value), emoji.Tag(right.GetTradeId()), right.GetName(), via)
		if r.GetVia() == nil {
			fmt.Fprintf(&b, "<i>range %s–%s · %d %s traded</i>\n\n", formatValue(low), formatValue(high), r.GetBaseVolume(), base.GetName())
		} else {
			fmt.Fprintf(&b, "<i>range %s–%s %s</i>\n\n", formatValue(low), formatValue(high), base.GetName())
		}
	}

	hour := time.Unix(resp.GetHourUtc(), 0).UTC()
	fmt.Fprintf(&b, "Hour from %s UTC\n", hour.Format("15:04 Jan 2"))
	fmt.Fprintf(&b, "<i>checked %s UTC</i>", time.Now().UTC().Format("15:04:05"))

	return b.String()
}
