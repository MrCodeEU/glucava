package analytics

import (
	"math"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// PercentRank returns the share of others (0-100) that v beats: values
// strictly below v count fully and ties count half, so the median of a
// list ranks at 50. It returns ok=false when there is nothing to compare to.
func PercentRank(others []float64, v float64) (pct float64, ok bool) {
	if len(others) == 0 {
		return 0, false
	}
	below, equal := 0, 0
	for _, o := range others {
		switch {
		case o < v:
			below++
		case o == v:
			equal++
		}
	}
	return (float64(below) + float64(equal)/2) / float64(len(others)) * 100, true
}

// Trend describes how fast glucose is moving right now.
type Trend struct {
	Rate  float64 // mg/dL per minute, positive when rising
	Arrow string  // "↑↑", "↑", "↗", "→", "↘", "↓" or "↓↓"
	Label string  // "rising fast", "steady", ...
	Code  string  // Label as a translation code: steady, rising, falling, rising_quickly, falling_quickly, rising_fast, falling_fast
	OK    bool    // false when there were not two readings far enough apart
}

// trendLookback is how far back the newest reading is compared against.
const trendLookback = 20 * time.Minute

// CurrentTrend compares the newest reading with the one closest to
// trendLookback earlier, using the bands of the common CGM arrows (1, 2 and
// 3 mg/dL per minute). Samples need not be sorted. With no reading between 5
// and 25 minutes older than the newest, OK is false.
func CurrentTrend(samples []stats.Sample) Trend {
	if len(samples) < 2 {
		return Trend{}
	}
	s := append([]stats.Sample(nil), samples...)
	sort.Slice(s, func(i, j int) bool { return s[i].Time.Before(s[j].Time) })
	last := s[len(s)-1]
	var ref *stats.Sample
	for i := len(s) - 2; i >= 0; i-- {
		age := last.Time.Sub(s[i].Time)
		if age > trendLookback+5*time.Minute {
			break
		}
		if age >= 5*time.Minute {
			ref = &s[i]
			if age >= trendLookback {
				break
			}
		}
	}
	if ref == nil {
		return Trend{}
	}
	rate := (last.Value - ref.Value) / last.Time.Sub(ref.Time).Minutes()
	t := Trend{Rate: rate, OK: true}
	mag := math.Abs(rate)
	rising := rate >= 0
	switch {
	case mag < 1:
		t.Arrow, t.Label, t.Code = "→", "steady", "steady"
	case mag < 2 && rising:
		t.Arrow, t.Label, t.Code = "↗", "rising", "rising"
	case mag < 2:
		t.Arrow, t.Label, t.Code = "↘", "falling", "falling"
	case mag < 3 && rising:
		t.Arrow, t.Label, t.Code = "↑", "rising quickly", "rising_quickly"
	case mag < 3:
		t.Arrow, t.Label, t.Code = "↓", "falling quickly", "falling_quickly"
	case rising:
		t.Arrow, t.Label, t.Code = "↑↑", "rising fast", "rising_fast"
	default:
		t.Arrow, t.Label, t.Code = "↓↓", "falling fast", "falling_fast"
	}
	return t
}
