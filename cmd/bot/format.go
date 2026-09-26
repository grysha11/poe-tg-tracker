package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/emoji"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

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

	if last := resp.GetLastFetchUtc(); last != 0 {
		fmt.Fprintf(&b, "Last fetch time: %s UTC\n", time.Unix(last, 0).UTC().Format("15:04 Jan 2"))
	}
	fmt.Fprintf(&b, "<i>Checked %s UTC</i>", time.Now().UTC().Format("15:04:05"))

	return b.String()
}
