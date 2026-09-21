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
	q      *dbgen.Queries
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

	dbase, err := db.Open(cfg.DBDSN)
	if err != nil {
		log.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer dbase.Close()

	base, quotes, err := loadDefaultRates(ctx, dbase.Q)
	if err != nil {
		log.Error("load default rate pairs failed", "err", err)
		os.Exit(1)
	}

	app := &App{
		bot:    telegram.NewBot(cfg.TelegramToken),
		client: exchange.NewClient(cfg.UserAgent),
		q:      dbase.Q,
		base:   base,
		quotes: quotes,
		cfg:    cfg,
		log:    log,
	}

	log.Info("starting", "league", cfg.League)

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

	for _, r := range rows {
		if !r.BaseItemPath.Valid || !r.QuoteItemPath.Valid {
			return exchange.Currency{}, nil, fmt.Errorf(
				"default_rate_pairs row (base_currency_id=%d, quote_currency_id=%d) references a missing currency",
				r.BaseCurrencyID, r.QuoteCurrencyID)
		}
	}

	base := exchange.Currency{ID: rows[0].BaseItemPath.String, Name: rows[0].BaseName.String, TradeID: rows[0].BaseTradeID.String}
	quotes := make([]exchange.Currency, 0, len(rows))
	for _, r := range rows {
		quotes = append(quotes, exchange.Currency{ID: r.QuoteItemPath.String, Name: r.QuoteName.String, TradeID: r.QuoteTradeID.String})
	}
	return base, quotes, nil
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
		a.send(ctx, msg.Chat.ID, text, telegram.RatesKeyboard("volume"))

	case "rates":
		text, err := a.buildRates(ctx, "volume")
		if err != nil {
			a.log.Error("rates fetch failed", "err", err)
			a.send(ctx, msg.Chat.ID, "Couldn't get rates right now. Try again shortly.", telegram.RatesKeyboard("volume"))
			return
		}
		a.send(ctx, msg.Chat.ID, text, telegram.RatesKeyboard("volume"))

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

	if cb.From == nil || !a.isWhitelisted(cb.From.ID) {
		var uid int64
		if cb.From != nil {
			uid = cb.From.ID
		}
		a.log.Warn("rejected callback: user not whitelisted", "user_id", uid)
		_ = a.bot.AnswerCallbackQuery(ctx, cb.ID, "Not authorized")
		return
	}

	prefix, view, hasView := strings.Cut(cb.Data, ":")
	if prefix != "rates" || (view != "volume" && view != "price") || !hasView || cb.Message == nil {
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

	if err := a.bot.EditMessageText(ctx, cb.Message.Chat.ID, cb.Message.MessageID, text, telegram.RatesKeyboard(view)); err != nil {
		a.log.Error("editMessageText failed", "err", err)
	}
}

func (a *App) send(ctx context.Context, chatID int64, text string, markup *telegram.InlineKeyboardMarkup) {
	if err := a.bot.SendMessage(ctx, chatID, text, markup); err != nil {
		a.log.Error("sendMessage failed", "err", err)
	}
}

func (a *App) buildRates(ctx context.Context, view string) (string, error) {
	hourUnix, err := a.q.LatestSnapshotHour(ctx, a.cfg.League)
	if err != nil {
		return "", err
	}
	if hourUnix == 0 {
		return "", fmt.Errorf("no snapshots yet for league %q", a.cfg.League)
	}

	dbRows, err := a.q.ListSnapshotRatesForHour(ctx, dbgen.ListSnapshotRatesForHourParams{
		HourUtc: hourUnix,
		League:  a.cfg.League,
	})
	if err != nil {
		return "", err
	}
	rows := toSnapshotRows(dbRows)

	var ranked []exchange.CurrencyRate
	if view == "price" {
		chaos, _ := quoteByTradeID(a.quotes, "chaos")
		exalt, _ := quoteByTradeID(a.quotes, "exalted")
		ranked = exchange.RankByPrice(rows, a.base, chaos, exalt, 10)
	} else {
		ranked = exchange.RankByVolume(rows, a.base, 10)
	}

	return formatRanked(view, a.base, ranked, time.Unix(hourUnix, 0).UTC(), a.cfg.League), nil
}

func toSnapshotRows(rows []dbgen.ListSnapshotRatesForHourRow) []exchange.SnapshotRow {
	out := make([]exchange.SnapshotRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, exchange.SnapshotRow{
			ItemA:         exchange.Currency{ID: r.ItemAPath, Name: r.ItemAName, TradeID: r.ItemATradeID},
			ItemB:         exchange.Currency{ID: r.ItemBPath, Name: r.ItemBName, TradeID: r.ItemBTradeID},
			VolumeA:       uint64(r.VolumeA),
			VolumeB:       uint64(r.VolumeB),
			LowestRatioA:  uint64(r.LowestRatioA),
			LowestRatioB:  uint64(r.LowestRatioB),
			HighestRatioA: uint64(r.HighestRatioA),
			HighestRatioB: uint64(r.HighestRatioB),
		})
	}
	return out
}

func quoteByTradeID(quotes []exchange.Currency, tradeID string) (exchange.Currency, bool) {
	for _, q := range quotes {
		if q.TradeID == tradeID {
			return q, true
		}
	}
	return exchange.Currency{}, false
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

func formatRanked(view string, base exchange.Currency, ranked []exchange.CurrencyRate, hour time.Time, league string) string {
	var b strings.Builder

	title, icon := "Top 10 by volume", "📊"
	if view == "price" {
		title, icon = "Most expensive", "💰"
	}
	fmt.Fprintf(&b, "%s <b>%s — %s</b>\n\n", icon, title, league)

	if len(ranked) == 0 {
		b.WriteString("No data for this hour yet.\n\n")
	}

	for _, cr := range ranked {
		via := ""
		if cr.Via != nil {
			via = fmt.Sprintf(" <i>(via %s)</i>", cr.Via.Name)
		}

		value, low, high := cr.Rate.VWAP, cr.Rate.Low, cr.Rate.High
		leftName, rightName := base.Name, cr.Currency.Name
		leftTradeID, rightTradeID := base.TradeID, cr.Currency.TradeID
		if value < 1 {
			invLow, invHigh := low, high
			if high > 0 {
				invLow = 1 / high
			}
			if low > 0 {
				invHigh = 1 / low
			}
			value, low, high = 1/value, invLow, invHigh
			leftName, rightName = cr.Currency.Name, base.Name
			leftTradeID, rightTradeID = cr.Currency.TradeID, base.TradeID
		}

		fmt.Fprintf(&b, "%s 1 %s = <b>%s</b> %s %s%s\n",
			emoji.Tag(leftTradeID), leftName, formatValue(value), emoji.Tag(rightTradeID), rightName, via)
		if cr.Via == nil {
			fmt.Fprintf(&b, "<i>range %s–%s · %d %s traded</i>\n\n", formatValue(low), formatValue(high), cr.Rate.BaseVol, base.Name)
		} else {
			fmt.Fprintf(&b, "<i>range %s–%s %s</i>\n\n", formatValue(low), formatValue(high), base.Name)
		}
	}

	fmt.Fprintf(&b, "Hour from %s UTC\n", hour.Format("15:04 Jan 2"))
	fmt.Fprintf(&b, "<i>checked %s UTC</i>", time.Now().UTC().Format("15:04:05"))

	return b.String()
}
