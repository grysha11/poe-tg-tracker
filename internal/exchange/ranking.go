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

func RankByVolume(rows []SnapshotRow, base Currency, limit int) []CurrencyRate {
	rated := currencyRatesFrom(rows, base)
	out := make([]CurrencyRate, 0, len(rated))
	for _, cr := range rated {
		out = append(out, cr)
	}
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

func RankByPrice(rows []SnapshotRow, base, chaos, exalt Currency, limit int) []CurrencyRate {
	direct := currencyRatesFrom(rows, base)

	result := make(map[string]CurrencyRate, len(direct))
	maps.Copy(result, direct)

	fillVia := func(via Currency) {
		baseToVia, ok := direct[via.ID]
		if !ok {
			return
		}
		for id, viaRate := range currencyRatesFrom(rows, via) {
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
				Via: &via,
			}
		}
	}
	fillVia(chaos)
	fillVia(exalt)

	out := make([]CurrencyRate, 0, len(result))
	for _, cr := range result {
		out = append(out, cr)
	}
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
