package main

import (
	"context"
	"fmt"
	"math/bits"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
	"github.com/grysha11/poe-tg-tracker/internal/telegram"
)

const (
	ratesLimit         = 10
	ratesCallback      = "rates"
	categoriesCallback = "cats"
	categoriesViewKey  = "categories"
	categoriesTTL      = 10 * time.Minute
	maxCategories      = 64
	uncategorized      = "uncategorized"
)

type rateView struct {
	key    string
	icon   string
	button string
	title  string
	rank   pb.RateView
}

var (
	volumeView = rateView{
		key:    "volume",
		icon:   "📊",
		button: "Top volume",
		title:  fmt.Sprintf("Top %d by volume", ratesLimit),
		rank:   pb.RateView_RATE_VIEW_VOLUME,
	}
	priceView = rateView{
		key:    "price",
		icon:   "💰",
		button: "Most expensive",
		title:  "Most expensive",
		rank:   pb.RateView_RATE_VIEW_PRICE,
	}

	rateViews = []rateView{volumeView, priceView}
)

func viewByKey(key string) (rateView, bool) {
	i := slices.IndexFunc(rateViews, func(v rateView) bool { return v.key == key })
	if i < 0 {
		return rateView{}, false
	}
	return rateViews[i], true
}

type selection struct {
	all  bool
	mask uint64
}

var allCategories = selection{all: true}

func parseSelection(s string) (selection, bool) {
	if s == "" {
		return allCategories, true
	}
	mask, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return selection{}, false
	}
	return selection{mask: mask}, true
}

func (s selection) String() string {
	if s.all {
		return ""
	}
	return strconv.FormatUint(s.mask, 16)
}

func fullMask(n int) uint64 {
	if n >= maxCategories {
		return ^uint64(0)
	}
	return 1<<n - 1
}

func (s selection) bits(n int) uint64 {
	if s.all {
		return fullMask(n)
	}
	return s.mask & fullMask(n)
}

func (s selection) has(i int) bool {
	return s.all || s.mask&(1<<i) != 0
}

func (s selection) toggle(i, n int) selection {
	mask := s.bits(n) ^ 1<<i
	if mask == fullMask(n) {
		return allCategories
	}
	return selection{mask: mask}
}

func (s selection) count(n int) int {
	return bits.OnesCount64(s.bits(n))
}

// names returns the selected categories, or nil when every category is selected.
func (s selection) names(cats []string) []string {
	if s.all || s.bits(len(cats)) == fullMask(len(cats)) {
		return nil
	}
	var out []string
	for i, c := range cats {
		if s.has(i) {
			out = append(out, c)
		}
	}
	return out
}

func categoryLabel(c string) string {
	if c == uncategorized {
		return "Other"
	}
	c = strings.NewReplacer("_", " ", "-", " ").Replace(c)
	return strings.ToUpper(c[:1]) + c[1:]
}

func callbackData(prefix string, view rateView, sel selection) string {
	data := prefix + ":" + view.key
	if !sel.all {
		data += ":" + sel.String()
	}
	return data
}

func viewButtons(active rateView, sel selection) []telegram.InlineKeyboardButton {
	row := make([]telegram.InlineKeyboardButton, 0, len(rateViews))
	for _, v := range rateViews {
		text := v.icon + " " + v.button
		if v.key == active.key {
			text = "• " + text
		}
		row = append(row, telegram.InlineKeyboardButton{Text: text, CallbackData: callbackData(ratesCallback, v, sel)})
	}
	return row
}

func ratesKeyboard(active rateView, sel selection, total int) *telegram.InlineKeyboardMarkup {
	label := "🗂 Categories"
	if !sel.all {
		label += fmt.Sprintf(" (%d/%d)", sel.count(total), total)
	}
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		viewButtons(active, sel),
		{{Text: label, CallbackData: callbackData(categoriesCallback, active, sel)}},
	}}
}

func categoriesKeyboard(active rateView, sel selection, cats []string) *telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton
	for i, c := range cats {
		mark := "▫️ "
		if sel.has(i) {
			mark = "✅ "
		}
		btn := telegram.InlineKeyboardButton{Text: mark + categoryLabel(c), CallbackData: callbackData(categoriesCallback, active, sel.toggle(i, len(cats)))}
		if i%2 == 0 {
			rows = append(rows, []telegram.InlineKeyboardButton{btn})
		} else {
			rows[len(rows)-1] = append(rows[len(rows)-1], btn)
		}
	}

	all := telegram.InlineKeyboardButton{Text: "Select all", CallbackData: callbackData(categoriesCallback, active, allCategories)}
	if sel.count(len(cats)) == len(cats) {
		all = telegram.InlineKeyboardButton{Text: "Unselect all", CallbackData: callbackData(categoriesCallback, active, selection{})}
	}
	rows = append(rows, []telegram.InlineKeyboardButton{all}, viewButtons(active, sel))
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (a *App) categories(ctx context.Context) ([]string, error) {
	if a.cats != nil && time.Since(a.catsAt) < categoriesTTL {
		return a.cats, nil
	}
	cats, err := a.gw.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	a.cats, a.catsAt = cats[:min(len(cats), maxCategories)], time.Now()
	return a.cats, nil
}
