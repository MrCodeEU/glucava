// Package stats computes glucose summary values for a time window.
package stats

import (
	"math"
	"sort"
	"strings"
	"time"
)

// Sample is one glucose reading in mg/dL.
type Sample struct {
	Time  time.Time
	Value float64
}

// MmolFactor converts mg/dL to mmol/L (divide) or back (multiply). Shared by
// the render package (display) and glucose importers (parsing readings that
// arrive in mmol/L), so there is exactly one place this ever changes.
const MmolFactor = 18.016

// Range is an inclusive target range in mg/dL. VeryLow and VeryHigh are the
// level-2 hypo/hyper thresholds; zero means the consensus defaults (54, 250),
// so a Range{Low, High} literal keeps working.
type Range struct {
	Low, High         float64
	VeryLow, VeryHigh float64
}

// Thresholds returns VeryLow and VeryHigh with the defaults applied.
func (r Range) Thresholds() (veryLow, veryHigh float64) {
	veryLow, veryHigh = r.VeryLow, r.VeryHigh
	if veryLow <= 0 {
		veryLow = veryLowThreshold
	}
	if veryHigh <= 0 {
		veryHigh = veryHighThreshold
	}
	return veryLow, veryHigh
}

// DefaultRange is the standard 70-180 mg/dL time-in-range band.
var DefaultRange = Range{Low: 70, High: 180}

// Summary holds the values shown in the Strava description.
type Summary struct {
	Count      int
	Min, Max   float64
	Avg        float64
	StdDev     float64 // population standard deviation of the samples, in mg/dL
	CV         float64 // coefficient of variation, percent: StdDev / Avg * 100
	GMI        float64 // Glucose Management Indicator, percent: an estimated A1C from the average glucose (ADA/ATTD formula, mg/dL input)
	TIR        float64 // percent of samples inside the range, 0-100
	Below      float64 // percent below Low
	Above      float64 // percent above High
	VeryLow    float64 // percent below Range.VeryLow (default 54 mg/dL): clinical "level 2" hypoglycemia
	VeryHigh   float64 // percent above Range.VeryHigh (default 250 mg/dL): clinical "level 2" hyperglycemia
	Start, End float64 // first and last value
}

// veryLowThreshold and veryHighThreshold are the standard ATTD/ADA
// consensus thresholds for clinically significant hypo- and
// hyperglycemia ("level 2"), independent of the user's own target Range.
const (
	veryLowThreshold  = 54.0
	veryHighThreshold = 250.0
)

// Within returns the samples inside the closed interval [from, to].
func Within(in []Sample, from, to time.Time) []Sample {
	out := make([]Sample, 0, len(in))
	for _, s := range in {
		if !s.Time.Before(from) && !s.Time.After(to) {
			out = append(out, s)
		}
	}
	return out
}

// Summarize returns statistics for samples. ok is false when samples is empty.
// A stored Summary keeps the very-low/very-high thresholds in force when it
// was computed; changing the settings only affects later computations.
func Summarize(samples []Sample, r Range) (s Summary, ok bool) {
	if len(samples) == 0 {
		return Summary{}, false
	}
	vlow, vhigh := r.Thresholds()
	sorted := append([]Sample(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	s.Count = len(sorted)
	s.Min = math.Inf(1)
	s.Max = math.Inf(-1)
	var sum float64
	var in, below, above, veryLow, veryHigh int
	for _, p := range sorted {
		sum += p.Value
		s.Min = math.Min(s.Min, p.Value)
		s.Max = math.Max(s.Max, p.Value)
		switch {
		case p.Value < r.Low:
			below++
		case p.Value > r.High:
			above++
		default:
			in++
		}
		if p.Value < vlow {
			veryLow++
		}
		if p.Value > vhigh {
			veryHigh++
		}
	}
	n := float64(s.Count)
	s.Avg = sum / n
	s.TIR = float64(in) / n * 100
	s.Below = float64(below) / n * 100
	s.Above = float64(above) / n * 100
	s.VeryLow = float64(veryLow) / n * 100
	s.VeryHigh = float64(veryHigh) / n * 100
	s.Start = sorted[0].Value
	s.End = sorted[len(sorted)-1].Value

	var sqDiff float64
	for _, p := range sorted {
		d := p.Value - s.Avg
		sqDiff += d * d
	}
	s.StdDev = math.Sqrt(sqDiff / n)
	if s.Avg > 0 {
		s.CV = s.StdDev / s.Avg * 100
	}
	// ADA/ATTD consensus formula: GMI(%) = 3.31 + 0.02392 * mean glucose (mg/dL).
	s.GMI = 3.31 + 0.02392*s.Avg
	return s, true
}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// Sparkline renders samples as a unicode sparkline of at most width runes.
// Samples are averaged into equal time-ordered buckets when there are more than width.
func Sparkline(samples []Sample, width int) string {
	if len(samples) == 0 || width <= 0 {
		return ""
	}
	sorted := append([]Sample(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	n := min(width, len(sorted))
	vals := make([]float64, n)
	for i := range n {
		lo := i * len(sorted) / n
		hi := (i + 1) * len(sorted) / n
		var sum float64
		for _, p := range sorted[lo:hi] {
			sum += p.Value
		}
		vals[i] = sum / float64(hi-lo)
	}

	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	out := make([]rune, n)
	for i, v := range vals {
		idx := 0
		if hi > lo {
			idx = int(math.Round((v - lo) / (hi - lo) * float64(len(sparkRunes)-1)))
		}
		out[i] = sparkRunes[idx]
	}
	return string(out)
}

// LooksLikeSparkline reports whether s is non-empty and made up only of the
// block characters Sparkline can produce. render.Merge uses it to recognise
// its own sparkline line by shape, without depending on the line after it
// being blank: some hosts (Strava's own description editor, observed 2026-09)
// collapse blank lines between saves, which makes a blank-line boundary
// unreliable.
func LooksLikeSparkline(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(string(sparkRunes), r) {
			return false
		}
	}
	return true
}
