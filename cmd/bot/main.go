package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/db"
	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
	"github.com/grysha11/poe-tg-tracker/internal/emoji"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
	"github.com/grysha11/poe-tg-tracker/internal/logger"
	"github.com/grysha11/poe-tg-tracker/internal/telegram"
)

type App struct {
	bot    *telegram.Bot
	client *exchange.Client
	cache  *exchange.Cache
	base   exchange.Currency
	quotes []exchange.Currency
	cfg    config.Config
	log    *slog.Logger
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

	dbase, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer dbase.Close()

	if err := dbase.Migrate(); err != nil {
		log.Error("db migrate failed", "err", err)
		os.Exit(1)
	}

	base, quotes, err := loadDefaultRates(ctx, dbase.Q)
	if err != nil {
		log.Error("load default rate pairs failed", "err", err)
		os.Exit(1)
	}

	client := exchange.NewClient(cfg.UserAgent)
	app := &App{
		bot:    telegram.NewBot(cfg.TelegramToken),
		client: client,
		cache:  exchange.NewCache(client, cfg.League, base.ID, quotes, cfg.CacheTTL),
		base:   base,
		quotes: quotes,
		cfg:    cfg,
		log:    log,
	}

	log.Info("starting", "league", cfg.League, "min_divine_vol", cfg.MinDivineVolume)

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

func loadDefaultRates(ctx context.Context, q *dbgen.Queries) (exchange.Currency, []exchange.Currency, error) {
	rows, err := q.ListDefaultRatePairs(ctx)
	if err != nil {
		return exchange.Currency{}, nil, err
	}
	if len(rows) == 0 {
		return exchange.Currency{}, nil, fmt.Errorf("no default rate pairs configured")
	}

	base := exchange.Currency{ID: rows[0].BaseItemPath, Name: rows[0].BaseName, TradeID: rows[0].BaseTradeID}
	quotes := make([]exchange.Currency, 0, len(rows))
	for _, r := range rows {
		quotes = append(quotes, exchange.Currency{ID: r.QuoteItemPath, Name: r.QuoteName, TradeID: r.QuoteTradeID})
	}
	return base, quotes, nil
}

func (a *App) handleMessage(ctx context.Context, msg *telegram.Message) {
	cmd, ok := telegram.Command(msg.Text)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	switch cmd {
	case "start", "help":
		text := strings.Join([]string{
			"<b>PoE2 rate tracker</b>",
			"",
			"Tap the button for Divine rates from the last settled hour.",
			"",
			"/rates — show rates",
			"/leagues — list league strings",
		}, "\n")
		a.send(ctx, msg.Chat.ID, text, telegram.RatesKeyboard())

	case "rates":
		snap, _, err := a.cache.Get(ctx)
		if err != nil {
			a.log.Error("rates fetch failed", "err", err)
			a.send(ctx, msg.Chat.ID, "Couldn't get rates right now. Try again shortly.", telegram.RatesKeyboard())
			return
		}
		a.send(ctx, msg.Chat.ID, a.formatRates(snap), telegram.RatesKeyboard())

	case "leagues":
		d, err := a.client.Fetch(ctx, exchange.AlignHour(time.Now()).Add(-time.Hour).Unix())
		if err != nil {
			a.log.Error("leagues fetch failed", "err", err)
			a.send(ctx, msg.Chat.ID, "Couldn't reach the exchange API.", nil)
			return
		}
		leagues := exchange.Leagues(d)
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

	if cb.Data != "rates" || cb.Message == nil {
		_ = a.bot.AnswerCallbackQuery(ctx, cb.ID, "")
		return
	}

	snap, fresh, err := a.cache.Get(ctx)
	if err != nil {
		a.log.Error("callback rates fetch failed", "err", err)
		_ = a.bot.AnswerCallbackQuery(ctx, cb.ID, "Couldn't reach the API")
		return
	}

	toast := "Already current"
	if fresh {
		toast = "Updated"
	}
	if err := a.bot.AnswerCallbackQuery(ctx, cb.ID, toast); err != nil {
		a.log.Error("answerCallbackQuery failed", "err", err)
	}

	if err := a.bot.EditMessageText(ctx, cb.Message.Chat.ID, cb.Message.MessageID,
		a.formatRates(snap), telegram.RatesKeyboard()); err != nil {
		a.log.Error("editMessageText failed", "err", err)
	}
}

func (a *App) send(ctx context.Context, chatID int64, text string, markup *telegram.InlineKeyboardMarkup) {
	if err := a.bot.SendMessage(ctx, chatID, text, markup); err != nil {
		a.log.Error("sendMessage failed", "err", err)
	}
}

func (a *App) formatRates(s *exchange.Snapshot) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s <b>%s</b>\n\n", emoji.Tag(a.base.TradeID), s.League)
	for _, q := range a.quotes {
		r, ok := s.Rates[q.ID]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s 1 %s = <b>%.2f</b> %s\n", emoji.Tag(q.TradeID), a.base.Name, r.VWAP, q.Name)
		fmt.Fprintf(&b, "<i>range %.1f–%.1f · %d %s traded</i>\n\n", r.Low, r.High, r.DivineVol, a.base.Name)
	}

	if s.Thin(a.cfg.MinDivineVolume) {
		fmt.Fprintf(&b, "⚠️ Thin hour — few trades, treat as indicative\n\n")
	}

	fmt.Fprintf(&b, "Hour from %s UTC\n", s.HourUTC.Format("15:04 Jan 2"))
	fmt.Fprintf(&b, "<i>checked %s UTC</i>", time.Now().UTC().Format("15:04:05"))

	return b.String()
}
