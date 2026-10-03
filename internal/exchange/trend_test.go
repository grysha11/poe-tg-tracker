package exchange

import (
	"slices"
	"testing"
)

func TestTrend(t *testing.T) {
	hours := []int64{100, 200, 300}
	rowsByHour := map[int64][]SnapshotRow{
		100: {
			{ItemA: divine, ItemB: chaos, VolumeA: 10, VolumeB: 1000, LowestRatioA: 10, LowestRatioB: 1000, HighestRatioA: 10, HighestRatioB: 1000},
			{ItemA: chaos, ItemB: annul, VolumeA: 100, VolumeB: 50, LowestRatioA: 100, LowestRatioB: 50, HighestRatioA: 100, HighestRatioB: 50},
		},
		300: {
			{ItemA: divine, ItemB: chaos, VolumeA: 10, VolumeB: 1500, LowestRatioA: 10, LowestRatioB: 1500, HighestRatioA: 10, HighestRatioB: 1500},
			{ItemA: divine, ItemB: mirror, VolumeA: 50, VolumeB: 10, LowestRatioA: 50, LowestRatioB: 10, HighestRatioA: 50, HighestRatioB: 10},
		},
	}

	got := Trend(rowsByHour, hours, divine, []Currency{chaos, exalt}, []string{chaos.ID, annul.ID, mirror.ID, gem.ID})

	want := map[string][]float64{
		chaos.ID:  {100, 0, 150},
		annul.ID:  {50, 0, 0},
		mirror.ID: {0, 0, 0.2},
		gem.ID:    {0, 0, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for id, w := range want {
		if !slices.Equal(got[id], w) {
			t.Errorf("%s = %v, want %v", id, got[id], w)
		}
	}
}

func TestTrendNoHours(t *testing.T) {
	got := Trend(nil, nil, divine, nil, []string{chaos.ID})
	if len(got[chaos.ID]) != 0 {
		t.Fatalf("got %v, want empty series", got)
	}
}
