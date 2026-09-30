package analytics

import (
	"math"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// AGPBinMinutes is the width of one time-of-day bin.
const AGPBinMinutes = 15

// AGPBins is the number of bins in a day (96).
const AGPBins = 24 * 60 / AGPBinMinutes

// DefaultAGPMinSamples is the bin population below which a bin is Sparse.
const DefaultAGPMinSamples = 5

// AGPBin holds the percentiles of every reading whose local time of day falls
// in [Minute, Minute+15). Percentiles use linear interpolation between the
// closest ranks (R-7, the spreadsheet PERCENTILE default).
type AGPBin struct {
	Minute int     `json:"minute"` // start of the bin, minutes after local midnight
	N      int     `json:"n"`
	P5     float64 `json:"p5"`
	P25    float64 `json:"p25"`
	P50    float64 `json:"p50"`
	P75    float64 `json:"p75"`
	P95    float64 `json:"p95"`
	// Sparse marks bins with fewer than the minimum samples; their
	// percentiles are computed from what exists (all zero when N == 0) and
	// should be drawn faded or skipped.
	Sparse bool `json:"sparse"`
}

// AGP is the Ambulatory Glucose Profile: a one-day curve of percentiles
// overlaying every day in the input by local wall-clock time.
type AGP struct {
	Bins [AGPBins]AGPBin `json:"bins"`
	Days int             `json:"days"` // distinct local calendar days with data
	N    int             `json:"n"`
}

// ComputeAGP bins by loc wall-clock time, so a DST change does not
// shift the curve: the affected day just has one bin-hour fewer or more.
// minSamples <= 0 means DefaultAGPMinSamples. A nil loc means UTC.
func ComputeAGP(samples []stats.Sample, loc *time.Location, minSamples int) AGP {
	if loc == nil {
		loc = time.UTC
	}
	if minSamples <= 0 {
		minSamples = DefaultAGPMinSamples
	}
	var perBin [AGPBins][]float64
	days := map[int]struct{}{}
	for _, s := range samples {
		t := s.Time.In(loc)
		h, m, _ := t.Clock()
		b := (h*60 + m) / AGPBinMinutes
		perBin[b] = append(perBin[b], s.Value)
		y, mo, d := t.Date()
		days[y*10000+int(mo)*100+d] = struct{}{}
	}
	var out AGP
	out.Days, out.N = len(days), len(samples)
	for i := range out.Bins {
		v := perBin[i]
		bin := AGPBin{Minute: i * AGPBinMinutes, N: len(v), Sparse: len(v) < minSamples}
		if len(v) > 0 {
			sort.Float64s(v)
			bin.P5, bin.P25, bin.P50 = percentile(v, 5), percentile(v, 25), percentile(v, 50)
			bin.P75, bin.P95 = percentile(v, 75), percentile(v, 95)
		}
		out.Bins[i] = bin
	}
	return out
}

// percentile returns the p-th percentile (0..100) of ascending-sorted v using
// linear interpolation (R-7). v must not be empty.
func percentile(v []float64, p float64) float64 {
	if len(v) == 1 {
		return v[0]
	}
	rank := p / 100 * float64(len(v)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return v[lo]
	}
	return v[lo] + (v[hi]-v[lo])*(rank-float64(lo))
}
