package main

import (
	"context"
	"fmt"
	"slices"

	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
	"github.com/grysha11/poe-tg-tracker/internal/telegram"
)

const (
	ratesLimit    = 10
	ratesCallback = "rates"
)

type rateView struct {
	key    string
	icon   string
	button string
	title  string
	load   func(ctx context.Context, a *App) (*pb.GetRatesResponse, error)
}

var (
	volumeView = rateView{
		key:    "volume",
		icon:   "📊",
		button: "Top volume",
		title:  fmt.Sprintf("Top %d by volume", ratesLimit),
		load:   rankedBy(pb.RateView_RATE_VIEW_VOLUME),
	}
	priceView = rateView{
		key:    "price",
		icon:   "💰",
		button: "Most expensive",
		title:  "Most expensive",
		load:   rankedBy(pb.RateView_RATE_VIEW_PRICE),
	}

	rateViews = []rateView{volumeView, priceView}
)

func rankedBy(view pb.RateView) func(context.Context, *App) (*pb.GetRatesResponse, error) {
	return func(ctx context.Context, a *App) (*pb.GetRatesResponse, error) {
		return a.gw.GetRates(ctx, a.cfg.League, view, ratesLimit)
	}
}

func viewByKey(key string) (rateView, bool) {
	i := slices.IndexFunc(rateViews, func(v rateView) bool { return v.key == key })
	if i < 0 {
		return rateView{}, false
	}
	return rateViews[i], true
}

func ratesKeyboard(active rateView) *telegram.InlineKeyboardMarkup {
	row := make([]telegram.InlineKeyboardButton, 0, len(rateViews))
	for _, v := range rateViews {
		text := v.icon + " " + v.button
		if v.key == active.key {
			text = "• " + text
		}
		row = append(row, telegram.InlineKeyboardButton{Text: text, CallbackData: ratesCallback + ":" + v.key})
	}
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{row}}
}
