package analytics

import (
	"math"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

var (
	th   = Thresholds{VeryLow: 54, Low: 70, High: 180, VeryHigh: 250}
	vie  = mustLoc("Europe/Vienna")
	base = time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
)

func mustLoc(n string) *time.Location {
	l, err := time.LoadLocation(n)
	if err != nil {
		panic(err)
	}
	return l
}

// series builds readings every step starting at start.
func series(start time.Time, step time.Duration, vals ...float64) []stats.Sample {
	out := make([]stats.Sample, len(vals))
	for i, v := range vals {
		out[i] = stats.Sample{Time: start.Add(time.Duration(i) * step), Value: v}
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestBandOfBoundaries(t *testing.T) {
	for _, tc := range []struct {
		v    float64
		want Band
	}{
		{53.9, BandVeryLow}, {54, BandLow}, {69.9, BandLow}, {70, BandInRange}, {180, BandInRange},
		{180.1, BandHigh}, {250, BandHigh}, {250.1, BandVeryHigh},
	} {
		if got := th.BandOf(tc.v); got != tc.want {
			t.Errorf("BandOf(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestFromRangeDefaults(t *testing.T) {
	if got := FromRange(stats.Range{Low: 70, High: 180}); got != th {
		t.Errorf("got %+v", got)
	}
	if got := FromRange(stats.Range{Low: 70, High: 180, VeryLow: 60, VeryHigh: 240}); got.VeryLow != 60 || got.VeryHigh != 240 {
		t.Errorf("got %+v", got)
	}
}

func TestTIR5AgreesWithSummarize(t *testing.T) {
	s := series(base, 5*time.Minute, 40, 54, 60, 70, 100, 180, 200, 250, 300, 120)
	got := ComputeTIR5(s, th)
	sum, _ := stats.Summarize(s, stats.Range{Low: 70, High: 180})
	if !near(got.Below(), sum.Below) || !near(got.Above(), sum.Above) || !near(got.InRange, sum.TIR) ||
		!near(got.VeryLow, sum.VeryLow) || !near(got.VeryHigh, sum.VeryHigh) {
		t.Errorf("tir5 %+v vs summary %+v", got, sum)
	}
	if total := got.VeryLow + got.Low + got.InRange + got.High + got.VeryHigh; !near(total, 100) {
		t.Errorf("bands sum to %v", total)
	}
	if got.Count != 10 || got.VeryLow != 10 || got.Low != 20 || got.InRange != 40 || got.High != 20 || got.VeryHigh != 10 {
		t.Errorf("got %+v", got)
	}
	if z := ComputeTIR5(nil, th); z != (TIR5{}) {
		t.Errorf("empty = %+v", z)
	}
}

func TestGRI(t *testing.T) {
	// All in range: 0, zone A.
	if g := ComputeGRI(TIR5{InRange: 100, Count: 10}); g.Score != 0 || g.Zone != "A" {
		t.Errorf("perfect = %+v", g)
	}
	// Klonoff worked form: 3.0*VLow + 2.4*Low + 1.6*VHigh + 0.8*High.
	tir := TIR5{VeryLow: 1, Low: 4, InRange: 70, High: 20, VeryHigh: 5, Count: 100}
	want := 3.0*1 + 2.4*4 + 1.6*5 + 0.8*20
	if g := ComputeGRI(tir); !near(g.Score, want) || g.Zone != "B" {
		t.Errorf("score = %v (%s), want %v (B)", g.Score, g.Zone, want)
	}
	if g := ComputeGRI(TIR5{VeryLow: 60, VeryHigh: 40, Count: 5}); g.Score != 100 || g.Zone != "E" {
		t.Errorf("cap = %+v", g)
	}
	if g := ComputeGRI(TIR5{}); g != (GRI{}) {
		t.Errorf("empty = %+v", g)
	}
}

func TestCoverage(t *testing.T) {
	from, to := base, base.Add(2*time.Hour)
	full := series(from, 5*time.Minute, make([]float64, 24)...)
	if c := ComputeCoverage(full, from, to, 0, 0); c.Pct != 100 || len(c.Gaps) != 0 || c.Expected != 24 {
		t.Errorf("full = %+v", c)
	}
	// Duplicates from a second source do not inflate coverage.
	if c := ComputeCoverage(append(full, full...), from, to, 0, 0); c.Pct != 100 {
		t.Errorf("dupes pct = %v", c.Pct)
	}
	// Silence 00:20..01:20 is one inner gap; the last reading lands on `to`.
	gappy := append(series(from, 5*time.Minute, make([]float64, 5)...), series(from.Add(80*time.Minute), 5*time.Minute, make([]float64, 9)...)...)
	c := ComputeCoverage(gappy, from, to, 0, 0)
	if len(c.Gaps) != 1 || c.Longest != 60*time.Minute || c.Gaps[0].Duration != 60*time.Minute {
		t.Errorf("gaps = %+v", c)
	}
	// Trailing silence beyond the threshold is a gap too.
	trail := ComputeCoverage(series(from, 5*time.Minute, make([]float64, 5)...), from, to, 0, 0)
	if len(trail.Gaps) != 1 || !trail.Gaps[0].End.Equal(to) || trail.Gaps[0].Duration != 100*time.Minute {
		t.Errorf("trailing = %+v", trail.Gaps)
	}
	// Gap exactly at threshold is not a gap (strictly greater).
	edge := series(from, 30*time.Minute, 100, 100, 100, 100, 100)
	if c := ComputeCoverage(edge, from, from.Add(2*time.Hour), 0, 0); len(c.Gaps) != 0 {
		t.Errorf("30 min gap counted: %+v", c.Gaps)
	}
	// Empty window data: one gap over everything; degenerate window: zero value.
	if c := ComputeCoverage(nil, from, to, 0, 0); len(c.Gaps) != 1 || c.Pct != 0 || c.Longest != 2*time.Hour {
		t.Errorf("empty = %+v", c)
	}
	if c := ComputeCoverage(full, to, from, 0, 0); c.Expected != 0 || c.Pct != 0 {
		t.Errorf("inverted = %+v", c)
	}
	// Readings outside the window are ignored.
	if c := ComputeCoverage(full, from.Add(time.Hour), to, 0, 0); c.Slots != 12 {
		t.Errorf("slots = %d", c.Slots)
	}
}

func TestWindows(t *testing.T) {
	s := series(base, time.Hour, 1, 2, 3, 4, 5)
	ws := []Window{{base.Add(time.Hour), base.Add(3 * time.Hour)}}
	if got := ExcludeWindows(s, ws); len(got) != 3 || got[0].Value != 1 || got[1].Value != 4 {
		t.Errorf("exclude = %+v", got) // [1h,3h) removes 2 and 3
	}
	if got := OnlyWindows(s, ws); len(got) != 2 || got[0].Value != 2 || got[1].Value != 3 {
		t.Errorf("only = %+v", got)
	}
	if got := ExcludeWindows(s, nil); len(got) != 5 {
		t.Errorf("no windows exclude = %d", len(got))
	}
	if got := OnlyWindows(s, nil); len(got) != 0 {
		t.Errorf("no windows only = %d", len(got))
	}
}

func TestUnsortedInputHandledAndNotMutated(t *testing.T) {
	s := series(base, 5*time.Minute, 60, 60, 60, 60, 100)
	rev := append([]stats.Sample(nil), s...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	first := rev[0]
	eps := DetectEpisodes(rev, th, EpisodeOptions{})
	if len(eps) != 1 || eps[0].Kind != KindLow || eps[0].Duration != 15*time.Minute {
		t.Errorf("eps = %+v", eps)
	}
	if rev[0] != first {
		t.Error("input mutated")
	}
}
