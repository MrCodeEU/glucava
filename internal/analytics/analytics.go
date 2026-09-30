package analytics

import (
	"math"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// Thresholds are the four glucose cut-offs in mg/dL:
// VeryLow < Low <= in range <= High < VeryHigh.
type Thresholds struct {
	VeryLow, Low, High, VeryHigh float64
}

// FromRange builds Thresholds from a stats.Range, resolving zero very
// low/high to the consensus defaults (54/250).
func FromRange(r stats.Range) Thresholds {
	vl, vh := r.Thresholds()
	return Thresholds{VeryLow: vl, Low: r.Low, High: r.High, VeryHigh: vh}
}

// Band is one of the five consensus glucose bands.
type Band int

// The bands, lowest first.
const (
	BandVeryLow Band = iota
	BandLow
	BandInRange
	BandHigh
	BandVeryHigh
)

// BandOf classifies one reading. VeryLow (<) and VeryHigh (>) are exclusive
// like in stats.Summarize, so a reading exactly at 54 is Low, not VeryLow.
func (t Thresholds) BandOf(v float64) Band {
	switch {
	case v < t.VeryLow:
		return BandVeryLow
	case v < t.Low:
		return BandLow
	case v <= t.High:
		return BandInRange
	case v <= t.VeryHigh:
		return BandHigh
	default:
		return BandVeryHigh
	}
}

// TIR5 is the five-band time in range, each in percent of readings
// (sample-weighted: every reading counts the same). The bands are disjoint
// and sum to 100 for Count > 0. Note stats.Summary.Below is VeryLow+Low here
// and Summary.Above is High+VeryHigh.
type TIR5 struct {
	VeryLow  float64 `json:"veryLow"`
	Low      float64 `json:"low"`
	InRange  float64 `json:"inRange"`
	High     float64 `json:"high"`
	VeryHigh float64 `json:"veryHigh"`
	Count    int     `json:"count"`
}

// Below is VeryLow + Low.
func (t TIR5) Below() float64 { return t.VeryLow + t.Low }

// Above is High + VeryHigh.
func (t TIR5) Above() float64 { return t.High + t.VeryHigh }

// ComputeTIR5 returns the five-band split; the zero TIR5 for no samples.
func ComputeTIR5(samples []stats.Sample, th Thresholds) TIR5 {
	var n [5]int
	for _, s := range samples {
		n[th.BandOf(s.Value)]++
	}
	return tir5FromCounts(n, len(samples))
}

func tir5FromCounts(n [5]int, total int) TIR5 {
	if total == 0 {
		return TIR5{}
	}
	f := 100 / float64(total)
	return TIR5{
		VeryLow: float64(n[BandVeryLow]) * f, Low: float64(n[BandLow]) * f, InRange: float64(n[BandInRange]) * f,
		High: float64(n[BandHigh]) * f, VeryHigh: float64(n[BandVeryHigh]) * f, Count: total,
	}
}

// GRI is the Glycemia Risk Index (Klonoff et al., 2023):
// 3.0*VeryLow + 2.4*Low + 1.6*VeryHigh + 0.8*High (percent bands, capped at
// 100), where Low is the 54..<70 band and High the >180..250 band.
type GRI struct {
	Score float64 `json:"score"`
	// Hypo = VeryLow + 0.8*Low and Hyper = VeryHigh + 0.5*High are the two
	// components plotted on the GRI grid; Score = 3.0*Hypo + 1.6*Hyper.
	Hypo  float64 `json:"hypo"`
	Hyper float64 `json:"hyper"`
	// Zone is "A" (lowest risk, score <= 20) through "E" (> 80).
	Zone string `json:"zone"`
}

// ComputeGRI derives the index from a five-band split. Zero for Count == 0.
func ComputeGRI(t TIR5) GRI {
	if t.Count == 0 {
		return GRI{}
	}
	hypo := t.VeryLow + 0.8*t.Low
	hyper := t.VeryHigh + 0.5*t.High
	score := math.Min(100, 3.0*hypo+1.6*hyper)
	zone := "E"
	switch {
	case score <= 20:
		zone = "A"
	case score <= 40:
		zone = "B"
	case score <= 60:
		zone = "C"
	case score <= 80:
		zone = "D"
	}
	return GRI{Score: score, Hypo: hypo, Hyper: hyper, Zone: zone}
}

// DefaultSampleInterval is the Dexcom reading cadence.
const DefaultSampleInterval = 5 * time.Minute

// DefaultGapThreshold is the silence after which a stretch counts as a gap.
const DefaultGapThreshold = 30 * time.Minute

// Gap is a stretch without readings longer than the gap threshold.
type Gap struct {
	Start    time.Time     `json:"start"`
	End      time.Time     `json:"end"`
	Duration time.Duration `json:"duration"`
}

// Coverage describes how complete the data is over [from, to].
type Coverage struct {
	// Pct is distinct interval slots that hold a reading, over the slots the
	// window spans, capped at 100. Duplicate readings from several sources in
	// the same slot count once.
	Pct      float64
	Slots    int // distinct slots with a reading
	Expected int // slots in the window
	Gaps     []Gap
	Longest  time.Duration // longest gap, zero when none
}

// ComputeCoverage measures data completeness in [from, to]. interval <= 0
// means DefaultSampleInterval, gapThreshold <= 0 means DefaultGapThreshold.
// Readings outside the window are ignored. Leading and trailing silence
// counts as a gap once it exceeds gapThreshold. A window with no readings is
// one gap spanning the window.
func ComputeCoverage(samples []stats.Sample, from, to time.Time, interval, gapThreshold time.Duration) Coverage {
	if interval <= 0 {
		interval = DefaultSampleInterval
	}
	if gapThreshold <= 0 {
		gapThreshold = DefaultGapThreshold
	}
	var c Coverage
	if !to.After(from) {
		return c
	}
	c.Expected = int(math.Ceil(float64(to.Sub(from)) / float64(interval)))
	in := sortedWithin(samples, from, to)

	slots := make(map[int64]struct{}, len(in))
	prev := from
	addGap := func(a, b time.Time) {
		if d := b.Sub(a); d > gapThreshold {
			c.Gaps = append(c.Gaps, Gap{Start: a, End: b, Duration: d})
			if d > c.Longest {
				c.Longest = d
			}
		}
	}
	for _, s := range in {
		slots[int64(s.Time.Sub(from)/interval)] = struct{}{}
		addGap(prev, s.Time)
		prev = s.Time
	}
	addGap(prev, to)
	c.Slots = len(slots)
	if c.Expected > 0 {
		c.Pct = math.Min(100, float64(c.Slots)/float64(c.Expected)*100)
	}
	return c
}

// Window is a half-open time span [Start, End) used to include or exclude
// readings, typically the activities of a day.
type Window struct{ Start, End time.Time }

func (w Window) contains(t time.Time) bool { return !t.Before(w.Start) && t.Before(w.End) }

// ExcludeWindows returns the readings outside every window, order preserved.
// This gives "excluding activity" stats when passed the activity spans.
func ExcludeWindows(samples []stats.Sample, ws []Window) []stats.Sample {
	return filterWindows(samples, ws, false)
}

// OnlyWindows returns the readings inside at least one window.
func OnlyWindows(samples []stats.Sample, ws []Window) []stats.Sample {
	return filterWindows(samples, ws, true)
}

func filterWindows(samples []stats.Sample, ws []Window, keepInside bool) []stats.Sample {
	if len(ws) == 0 {
		if keepInside {
			return nil
		}
		return append([]stats.Sample(nil), samples...)
	}
	// Sorted, merged windows would allow a merge-walk, but window counts are
	// tiny (activities) and this stays obviously correct for overlaps.
	out := make([]stats.Sample, 0, len(samples))
	for _, s := range samples {
		inside := false
		for _, w := range ws {
			if w.contains(s.Time) {
				inside = true
				break
			}
		}
		if inside == keepInside {
			out = append(out, s)
		}
	}
	return out
}

// isSorted reports whether samples are in non-decreasing time order.
func isSorted(s []stats.Sample) bool {
	return sort.SliceIsSorted(s, func(i, j int) bool { return s[i].Time.Before(s[j].Time) })
}

// sortedCopy returns samples in time order: the input itself when already
// ordered, otherwise a sorted copy. Callers must not modify the result.
func sortedCopy(samples []stats.Sample) []stats.Sample {
	if isSorted(samples) {
		return samples
	}
	c := append([]stats.Sample(nil), samples...)
	sort.SliceStable(c, func(i, j int) bool { return c[i].Time.Before(c[j].Time) })
	return c
}

// sortedWithin returns the time-ordered readings with from <= t <= to.
func sortedWithin(samples []stats.Sample, from, to time.Time) []stats.Sample {
	s := sortedCopy(samples)
	lo := sort.Search(len(s), func(i int) bool { return !s[i].Time.Before(from) })
	hi := sort.Search(len(s), func(i int) bool { return s[i].Time.After(to) })
	return s[lo:hi]
}
