package exchange

import (
	"maps"
	"sort"
)

func currencyRatesFrom(rows []SnapshotRow, base Currency) map[string]CurrencyRate {
	out := map[string]CurrencyRate{}
	for _, row := range rows {
		var quote Currency
		var baseVol, quoteVol, baseLow, quoteLow, baseHigh, quoteHigh uint64
		switch base.ID {
		case row.ItemA.ID:
			quote = row.ItemB
			baseVol, quoteVol = row.VolumeA, row.VolumeB
			baseLow, quoteLow = row.LowestRatioA, row.LowestRatioB
			baseHigh, quoteHigh = row.HighestRatioA, row.HighestRatioB
		case row.ItemB.ID:
			quote = row.ItemA
			baseVol, quoteVol = row.VolumeB, row.VolumeA
			baseLow, quoteLow = row.LowestRatioB, row.LowestRatioA
			baseHigh, quoteHigh = row.HighestRatioB, row.HighestRatioA
		default:
			continue
		}
		if rate, ok := computeRate(quote.ID, baseVol, quoteVol, baseLow, quoteLow, baseHigh, quoteHigh); ok {
			out[quote.ID] = CurrencyRate{Currency: quote, Rate: rate}
		}
	}
	return out
}

const Uncategorized = "uncategorized"

type Categories map[string]bool

func (c Categories) keep(cur Currency) bool {
	return c == nil || c[cur.Category]
}

func filtered(rated map[string]CurrencyRate, cats Categories) []CurrencyRate {
	out := make([]CurrencyRate, 0, len(rated))
	for _, cr := range rated {
		if cats.keep(cr.Currency) {
			out = append(out, cr)
		}
	}
	return out
}

func RankByVolume(rows []SnapshotRow, base Currency, limit int, cats Categories) []CurrencyRate {
	out := filtered(currencyRatesFrom(rows, base), cats)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rate.BaseVol != out[j].Rate.BaseVol {
			return out[i].Rate.BaseVol > out[j].Rate.BaseVol
		}
		return out[i].Currency.ID < out[j].Currency.ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// RatesByID returns every currency priced in base, directly or through one of via, keyed by currency ID.
func RatesByID(rows []SnapshotRow, base Currency, via []Currency) map[string]CurrencyRate {
	direct := currencyRatesFrom(rows, base)

	result := make(map[string]CurrencyRate, len(direct))
	maps.Copy(result, direct)

	for _, v := range via {
		baseToVia, ok := direct[v.ID]
		if !ok {
			continue
		}
		for id, viaRate := range currencyRatesFrom(rows, v) {
			if id == base.ID {
				continue
			}
			if _, already := result[id]; already {
				continue
			}
			result[id] = CurrencyRate{
				Currency: viaRate.Currency,
				Rate: Rate{
					Quote:    viaRate.Currency.ID,
					VWAP:     baseToVia.Rate.VWAP * viaRate.Rate.VWAP,
					Low:      baseToVia.Rate.Low * viaRate.Rate.Low,
					High:     baseToVia.Rate.High * viaRate.Rate.High,
					BaseVol:  viaRate.Rate.BaseVol,
					QuoteVol: viaRate.Rate.QuoteVol,
				},
				Via: &v,
			}
		}
	}
	return result
}

func RankByPrice(rows []SnapshotRow, base Currency, via []Currency, limit int, cats Categories) []CurrencyRate {
	out := filtered(RatesByID(rows, base, via), cats)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rate.VWAP != out[j].Rate.VWAP {
			return out[i].Rate.VWAP < out[j].Rate.VWAP
		}
		return out[i].Currency.ID < out[j].Currency.ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
