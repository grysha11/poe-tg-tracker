package exchange

func Trend(rowsByHour map[int64][]SnapshotRow, hours []int64, base Currency, via []Currency, ids []string) map[string][]float64 {
	out := make(map[string][]float64, len(ids))
	for _, id := range ids {
		out[id] = make([]float64, len(hours))
	}
	for i, h := range hours {
		rows, ok := rowsByHour[h]
		if !ok {
			continue
		}
		rates := RatesByID(rows, base, via)
		for _, id := range ids {
			out[id][i] = rates[id].Rate.VWAP
		}
	}
	return out
}
