package analytics

import (
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

func TestEpisodeMinimumDuration(t *testing.T) {
	for _, tc := range []struct {
		name string
		vals []float64
		want int
	}{
		{"three readings span 10 min", []float64{60, 60, 60, 100}, 0},
		{"four readings span exactly 15 min", []float64{60, 60, 60, 60, 100}, 1},
		{"lone reading", []float64{100, 50, 100}, 0},
		{"still low at end of data", []float64{100, 60, 60, 60, 60}, 1},
		{"broken by in-range reading", []float64{60, 60, 100, 60, 60}, 0},
	} {
		eps := OfKind(DetectEpisodes(series(base, 5*time.Minute, tc.vals...), th, EpisodeOptions{}), KindLow)
		if len(eps) != tc.want {
			t.Errorf("%s: %d low episodes, want %d (%+v)", tc.name, len(eps), tc.want, eps)
		}
	}
}

func TestEpisodeFields(t *testing.T) {
	s := series(base.Add(time.Hour), 5*time.Minute, 100, 65, 48, 52, 66, 100)
	eps := DetectEpisodes(s, th, EpisodeOptions{})
	low, vlow := OfKind(eps, KindLow), OfKind(eps, KindVeryLow)
	if len(low) != 1 {
		t.Fatalf("eps = %+v", eps)
	}
	e := low[0]
	if e.Count != 4 || e.Extreme != 48 || e.Duration != 15*time.Minute || !e.Start.Equal(s[1].Time) || !e.End.Equal(s[4].Time) || !e.Nocturnal {
		t.Errorf("low = %+v", e)
	}
	// Very low (48, 52 -> 5 min span) is too short and is dropped on its own.
	if len(vlow) != 0 {
		t.Errorf("very low = %+v", vlow)
	}
	// Sustained very low nests inside the low episode.
	s = series(base, 5*time.Minute, 100, 50, 45, 50, 52, 100)
	eps = DetectEpisodes(s, th, EpisodeOptions{})
	if len(OfKind(eps, KindLow)) != 1 || len(OfKind(eps, KindVeryLow)) != 1 {
		t.Errorf("nesting: %+v", eps)
	}
	if eps[0].Kind != KindVeryLow && eps[0].Kind != KindLow {
		t.Errorf("order: %+v", eps)
	}
}

func TestEpisodeMaxGapSplits(t *testing.T) {
	a := series(base, 5*time.Minute, 60, 60, 60, 60)                     // 00:00-00:15
	b := series(base.Add(45*time.Minute), 5*time.Minute, 60, 60, 60, 60) // 00:45-01:00
	eps := DetectEpisodes(append(a, b...), th, EpisodeOptions{})
	if len(eps) != 2 {
		t.Fatalf("gap 30 min should split: %+v", eps)
	}
	// Gap of exactly 20 min (allowed) joins.
	c := series(base.Add(35*time.Minute), 5*time.Minute, 60, 60, 60, 60)
	eps = DetectEpisodes(append(a, c...), th, EpisodeOptions{})
	if len(eps) != 1 || eps[0].Duration != 50*time.Minute || eps[0].Count != 8 {
		t.Errorf("20 min gap should join: %+v", eps)
	}
}

func TestEpisodeHighKinds(t *testing.T) {
	s := series(base.Add(12*time.Hour), 5*time.Minute, 200, 260, 300, 270, 190, 100)
	eps := DetectEpisodes(s, th, EpisodeOptions{})
	high, vhigh := OfKind(eps, KindHigh), OfKind(eps, KindVeryHigh)
	if len(high) != 1 || high[0].Extreme != 300 || high[0].Count != 5 || high[0].Nocturnal {
		t.Errorf("high = %+v", high)
	}
	if len(vhigh) != 0 { // 260..270 spans only 10 min
		t.Errorf("veryHigh = %+v", vhigh)
	}
}

func TestEpisodeNocturnalUsesLocalTime(t *testing.T) {
	// 22:30 UTC on 2026-09-20 is 00:30 Vienna: nocturnal there, not in UTC.
	s := series(time.Date(2026, 9, 20, 22, 30, 0, 0, time.UTC), 5*time.Minute, 60, 60, 60, 60)
	if e := DetectEpisodes(s, th, EpisodeOptions{Loc: vie}); len(e) != 1 || !e[0].Nocturnal {
		t.Errorf("vienna: %+v", e)
	}
	if e := DetectEpisodes(s, th, EpisodeOptions{}); len(e) != 1 || e[0].Nocturnal {
		// 22:30 UTC start hour is 22, not < 6
		t.Errorf("utc: %+v", e)
	}
	// 05:59 is nocturnal, 06:00 is not.
	at := func(h, m int) bool {
		s := series(time.Date(2026, 9, 21, h, m, 0, 0, vie), 5*time.Minute, 60, 60, 60, 60)
		return DetectEpisodes(s, th, EpisodeOptions{Loc: vie})[0].Nocturnal
	}
	if !at(5, 59) || at(6, 0) || !at(0, 0) {
		t.Error("nocturnal boundaries wrong")
	}
}

func TestStartingAfterAndSummarize(t *testing.T) {
	end := base.Add(time.Hour)
	eps := []Episode{
		{Kind: KindLow, Start: end.Add(-10 * time.Minute), Duration: 30 * time.Minute, Extreme: 60}, // already running
		{Kind: KindLow, Start: end.Add(30 * time.Minute), Duration: 20 * time.Minute, Extreme: 55, Nocturnal: true},
		{Kind: KindLow, Start: end.Add(4 * time.Hour), Duration: 15 * time.Minute, Extreme: 65},
		{Kind: KindLow, Start: end.Add(12 * time.Hour), Duration: 15 * time.Minute, Extreme: 40},
	}
	got := StartingAfter(eps, end, 4*time.Hour)
	if len(got) != 2 || got[0].Extreme != 55 || got[1].Extreme != 65 {
		t.Errorf("after = %+v", got)
	}
	st := SummarizeEpisodes(eps)
	if st.Count != 4 || st.Nocturnal != 1 || st.Total != 80*time.Minute || st.Longest != 30*time.Minute || st.Extreme != 40 {
		t.Errorf("stats = %+v", st)
	}
	if z := SummarizeEpisodes(nil); z != (EpisodeStats{}) {
		t.Errorf("empty = %+v", z)
	}
	hs := SummarizeEpisodes([]Episode{{Kind: KindHigh, Extreme: 200}, {Kind: KindHigh, Extreme: 310}})
	if hs.Extreme != 310 {
		t.Errorf("high extreme = %v", hs.Extreme)
	}
}

func TestEpisodesEmptyAndDeterministicOrder(t *testing.T) {
	if e := DetectEpisodes(nil, th, EpisodeOptions{}); len(e) != 0 {
		t.Errorf("empty = %+v", e)
	}
	s := series(base, 5*time.Minute, 40, 40, 40, 40, 40)
	eps := DetectEpisodes(s, th, EpisodeOptions{})
	if len(eps) != 2 || eps[0].Kind != KindVeryLow || eps[1].Kind != KindLow {
		t.Errorf("tie order = %+v", eps)
	}
}

var _ = stats.Sample{}
