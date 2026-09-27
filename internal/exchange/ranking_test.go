package exchange

import "testing"

var (
	divine = Currency{ID: "Metadata/Items/Currency/CurrencyModValues", Name: "Divine Orb", TradeID: "divine"}
	chaos  = Currency{ID: "Metadata/Items/Currency/CurrencyRerollRare", Name: "Chaos Orb", TradeID: "chaos"}
	exalt  = Currency{ID: "Metadata/Items/Currency/CurrencyAddModToRare", Name: "Exalted Orb", TradeID: "exalted"}
	mirror = Currency{ID: "Metadata/Items/Currency/CurrencyDuplicate", Name: "Mirror of Kalandra", TradeID: "mirror"}
	annul  = Currency{ID: "Metadata/Items/Currency/CurrencyAnnulment", Name: "Orb of Annulment", TradeID: "annul"}
	gem    = Currency{ID: "Metadata/Items/Gems/SomeSupportGem", Name: "Some Gem", TradeID: "gem"}
)

func TestRankByVolume(t *testing.T) {
	t.Run("empty rows", func(t *testing.T) {
		if got := RankByVolume(nil, divine, 10); len(got) != 0 {
			t.Fatalf("RankByVolume(nil) = %v, want empty", got)
		}
	})

	rows := []SnapshotRow{
		{ItemA: divine, ItemB: chaos, VolumeA: 10, VolumeB: 1500, LowestRatioA: 10, LowestRatioB: 1400, HighestRatioA: 10, HighestRatioB: 1600},
		{ItemA: divine, ItemB: exalt, VolumeA: 30, VolumeB: 600, LowestRatioA: 30, LowestRatioB: 580, HighestRatioA: 30, HighestRatioB: 620},
		{ItemA: divine, ItemB: mirror, VolumeA: 0, VolumeB: 0},
		{ItemA: divine, ItemB: gem, VolumeA: 100, VolumeB: 5},
	}

	t.Run("ranks by base volume descending, non-currency items included", func(t *testing.T) {
		got := RankByVolume(rows, divine, 10)
		want := []string{"gem", "exalted", "chaos"}
		if len(got) != len(want) {
			t.Fatalf("len(got) = %d, want %d (zero-volume mirror excluded): %+v", len(got), len(want), got)
		}
		for i := range want {
			if got[i].Currency.TradeID != want[i] {
				t.Fatalf("got[%d] = %s, want %s (order by base volume 100 > 30 > 10)", i, got[i].Currency.TradeID, want[i])
			}
		}
	})

	t.Run("limit truncates", func(t *testing.T) {
		got := RankByVolume(rows, divine, 1)
		if len(got) != 1 {
			t.Fatalf("len(got) = %d, want 1", len(got))
		}
		if got[0].Currency.TradeID != "gem" {
			t.Fatalf("got[0] = %s, want gem (highest volume)", got[0].Currency.TradeID)
		}
	})

	t.Run("VWAP computed as quoteVol/baseVol", func(t *testing.T) {
		got := RankByVolume(rows, divine, 10)
		for _, cr := range got {
			if cr.Currency.TradeID == "chaos" && cr.Rate.VWAP != 150 {
				t.Errorf("chaos VWAP = %v, want 150 (1500/10)", cr.Rate.VWAP)
			}
		}
	})
}

func TestRankByPrice(t *testing.T) {
	t.Run("empty rows", func(t *testing.T) {
		if got := RankByPrice(nil, divine, []Currency{chaos, exalt}, 10); len(got) != 0 {
			t.Fatalf("RankByPrice(nil) = %v, want empty", got)
		}
	})

	rows := []SnapshotRow{
		{ItemA: divine, ItemB: chaos, VolumeA: 10, VolumeB: 1500, LowestRatioA: 10, LowestRatioB: 1500, HighestRatioA: 10, HighestRatioB: 1500},
		{ItemA: divine, ItemB: exalt, VolumeA: 30, VolumeB: 600, LowestRatioA: 30, LowestRatioB: 600, HighestRatioA: 30, HighestRatioB: 600},
		{ItemA: divine, ItemB: mirror, VolumeA: 50, VolumeB: 10, LowestRatioA: 50, LowestRatioB: 10, HighestRatioA: 50, HighestRatioB: 10},
		{ItemA: chaos, ItemB: annul, VolumeA: 100, VolumeB: 50, LowestRatioA: 100, LowestRatioB: 50, HighestRatioA: 100, HighestRatioB: 50},
	}

	t.Run("sorts ascending by VWAP (lowest = most expensive)", func(t *testing.T) {
		got := RankByPrice(rows, divine, []Currency{chaos, exalt}, 10)
		var order []string
		for _, cr := range got {
			order = append(order, cr.Currency.TradeID)
		}
		want := []string{"mirror", "exalted", "annul", "chaos"}
		if len(order) != len(want) {
			t.Fatalf("order = %v, want %v", order, want)
		}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("order = %v, want %v", order, want)
			}
		}
	})

	t.Run("routes via chaos when no direct rate exists", func(t *testing.T) {
		got := RankByPrice(rows, divine, []Currency{chaos, exalt}, 10)
		var annulRate *CurrencyRate
		for i := range got {
			if got[i].Currency.TradeID == "annul" {
				annulRate = &got[i]
			}
		}
		if annulRate == nil {
			t.Fatal("annul not present in ranked output")
		}
		if annulRate.Via == nil || annulRate.Via.TradeID != "chaos" {
			t.Fatalf("annul.Via = %v, want chaos", annulRate.Via)
		}
		if annulRate.Rate.VWAP != 75 {
			t.Errorf("annul VWAP = %v, want 75 (150 * 0.5)", annulRate.Rate.VWAP)
		}
	})

	t.Run("direct rate is never overridden by a via rate", func(t *testing.T) {
		got := RankByPrice(rows, divine, []Currency{chaos, exalt}, 10)
		for _, cr := range got {
			if cr.Currency.TradeID == "chaos" && cr.Via != nil {
				t.Errorf("chaos has a direct rate, Via should be nil, got %v", cr.Via)
			}
		}
	})

	t.Run("limit truncates the most expensive first", func(t *testing.T) {
		got := RankByPrice(rows, divine, []Currency{chaos, exalt}, 1)
		if len(got) != 1 || got[0].Currency.TradeID != "mirror" {
			t.Fatalf("got = %+v, want just mirror (lowest VWAP)", got)
		}
	})
}
