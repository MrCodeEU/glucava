package stats

import (
	"math"
	"time"
)

// HRSample is one heart rate reading.
type HRSample struct {
	Time time.Time
	BPM  float64
}

// HRSummary holds heart rate numbers for a time window.
type HRSummary struct {
	Count    int
	Min, Max float64
	Avg      float64
}

// SummarizeHR returns statistics for the readings inside [from, to]. ok is
// false when none fall inside. A zero from or to leaves that side open.
func SummarizeHR(samples []HRSample, from, to time.Time) (s HRSummary, ok bool) {
	s.Min, s.Max = math.Inf(1), math.Inf(-1)
	var sum float64
	for _, p := range samples {
		if (!from.IsZero() && p.Time.Before(from)) || (!to.IsZero() && p.Time.After(to)) {
			continue
		}
		s.Count++
		sum += p.BPM
		s.Min, s.Max = math.Min(s.Min, p.BPM), math.Max(s.Max, p.BPM)
	}
	if s.Count == 0 {
		return HRSummary{}, false
	}
	s.Avg = sum / float64(s.Count)
	return s, true
}
