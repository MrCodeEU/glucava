// Package stats computes glucose summary values for a time window.
package stats

import (
	"math"
	"sort"
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

// Range is an inclusive target range in mg/dL.
type Range struct {
	Low, High float64
}

// DefaultRange is the standard 70-180 mg/dL time-in-range band.
var DefaultRange = Range{Low: 70, High: 180}

// Summary holds the values shown in the Strava description.
type Summary struct {
	Count      int
	Min, Max   float64
	Avg        float64
	TIR        float64 // percent of samples inside the range, 0-100
	Below      float64 // percent below Low
	Above      float64 // percent above High
	Start, End float64 // first and last value
}

// Summarize returns statistics for samples. ok is false when samples is empty.
func Summarize(samples []Sample, r Range) (s Summary, ok bool) {
	if len(samples) == 0 {
		return Summary{}, false
	}
	sorted := append([]Sample(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	s.Count = len(sorted)
	s.Min = math.Inf(1)
	s.Max = math.Inf(-1)
	var sum float64
	var in, below, above int
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
	}
	n := float64(s.Count)
	s.Avg = sum / n
	s.TIR = float64(in) / n * 100
	s.Below = float64(below) / n * 100
	s.Above = float64(above) / n * 100
	s.Start = sorted[0].Value
	s.End = sorted[len(sorted)-1].Value
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
