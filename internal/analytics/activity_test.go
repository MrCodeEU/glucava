package analytics

import (
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

func TestPercentRank(t *testing.T) {
	if _, ok := PercentRank(nil, 5); ok {
		t.Error("empty others should not rank")
	}
	others := []float64{50, 60, 70, 80}
	cases := []struct {
		v, want float64
	}{{90, 100}, {40, 0}, {65, 50}, {70, 62.5}}
	for _, c := range cases {
		got, ok := PercentRank(others, c.v)
		if !ok || got != c.want {
			t.Errorf("PercentRank(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestCurrentTrend(t *testing.T) {
	t0 := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	mk := func(vals ...float64) []stats.Sample {
		var out []stats.Sample
		for i, v := range vals { // one reading every 5 minutes
			out = append(out, stats.Sample{Time: t0.Add(time.Duration(i) * 5 * time.Minute), Value: v})
		}
		return out
	}
	cases := []struct {
		name  string
		s     []stats.Sample
		arrow string
		ok    bool
	}{
		{"steady", mk(100, 101, 100, 102, 101), "→", true},
		{"rising", mk(100, 106, 112, 118, 124), "↗", true},
		{"rising quickly", mk(100, 112, 124, 136, 148), "↑", true},
		{"rising fast", mk(100, 120, 140, 160, 180), "↑↑", true},
		{"falling", mk(124, 118, 112, 106, 100), "↘", true},
		{"falling quickly", mk(148, 136, 124, 112, 100), "↓", true},
		{"falling fast", mk(180, 160, 140, 120, 100), "↓↓", true},
		{"single reading", mk(100), "", false},
		{"two readings five minutes apart", mk(100, 101), "→", true},
	}
	for _, c := range cases {
		got := CurrentTrend(c.s)
		if got.OK != c.ok || got.Arrow != c.arrow {
			t.Errorf("%s: got %+v, want arrow %q ok %v", c.name, got, c.arrow, c.ok)
		}
	}
	close := []stats.Sample{{Time: t0, Value: 100}, {Time: t0.Add(2 * time.Minute), Value: 110}}
	if CurrentTrend(close).OK {
		t.Error("readings two minutes apart should not give a trend")
	}
	// A reading far older than the lookback is not a trend.
	old := []stats.Sample{{Time: t0, Value: 100}, {Time: t0.Add(2 * time.Hour), Value: 180}}
	if CurrentTrend(old).OK {
		t.Error("readings two hours apart should not give a trend")
	}
}
