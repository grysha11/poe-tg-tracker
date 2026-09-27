package exchangesvc

import (
	dbgen "github.com/grysha11/poe-tg-tracker/internal/db/gen"
	"github.com/grysha11/poe-tg-tracker/internal/exchange"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

type ratePair struct {
	base, quote exchange.Currency
	sortOrder   int32
}

func currencyFromDB(c dbgen.Currency) exchange.Currency {
	return exchange.Currency{ID: c.ItemPath, Name: c.Name, TradeID: c.TradeID}
}

func ratePairFromDB(r dbgen.ListDefaultRatePairsRow) ratePair {
	return ratePair{
		base:      exchange.Currency{ID: r.BaseItemPath.String, Name: r.BaseName.String, TradeID: r.BaseTradeID.String},
		quote:     exchange.Currency{ID: r.QuoteItemPath.String, Name: r.QuoteName.String, TradeID: r.QuoteTradeID.String},
		sortOrder: int32(r.SortOrder),
	}
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

func toCurrencyRef(c exchange.Currency) *pb.CurrencyRef {
	return &pb.CurrencyRef{ItemPath: c.ID, Name: c.Name, TradeId: c.TradeID}
}

func toDefaultRatePairs(pairs []ratePair) []*pb.DefaultRatePair {
	out := make([]*pb.DefaultRatePair, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, &pb.DefaultRatePair{
			Base:      toCurrencyRef(p.base),
			Quote:     toCurrencyRef(p.quote),
			SortOrder: p.sortOrder,
		})
	}
	return out
}

func toRankedRates(ranked []exchange.CurrencyRate) []*pb.RankedRate {
	out := make([]*pb.RankedRate, 0, len(ranked))
	for _, cr := range ranked {
		rr := &pb.RankedRate{
			Currency:    toCurrencyRef(cr.Currency),
			Vwap:        cr.Rate.VWAP,
			Low:         cr.Rate.Low,
			High:        cr.Rate.High,
			BaseVolume:  cr.Rate.BaseVol,
			QuoteVolume: cr.Rate.QuoteVol,
		}
		if cr.Via != nil {
			rr.Via = toCurrencyRef(*cr.Via)
		}
		out = append(out, rr)
	}
	return out
}
