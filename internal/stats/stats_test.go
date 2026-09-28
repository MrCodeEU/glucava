package stats

import (
	"math"
	"testing"
	"time"
)

func samples(vals ...float64) []Sample {
	t0 := time.Date(2026, 9, 20, 7, 0, 0, 0, time.UTC)
	out := make([]Sample, len(vals))
	for i, v := range vals {
		out[i] = Sample{Time: t0.Add(time.Duration(i) * 5 * time.Minute), Value: v}
	}
	return out
}

func TestWithinIsInclusiveOfBothEnds(t *testing.T) {
	ss := samples(100, 120, 140, 160) // t0, t0+5m, t0+10m, t0+15m
	from, to := ss[1].Time, ss[2].Time
	got := Within(ss, from, to)
	if len(got) != 2 || got[0].Value != 120 || got[1].Value != 140 {
		t.Fatalf("Within(...) = %+v, want the two samples at the exact bounds", got)
	}
	if len(Within(ss, ss[3].Time.Add(time.Minute), ss[3].Time.Add(2*time.Minute))) != 0 {
		t.Error("a window after every sample should return none")
	}
}

func TestSummarizeEmpty(t *testing.T) {
	if _, ok := Summarize(nil, DefaultRange); ok {
		t.Fatal("expected ok=false for empty input")
	}
}

func TestSummarize(t *testing.T) {
	s, ok := Summarize(samples(60, 70, 100, 180, 200), DefaultRange)
	if !ok {
		t.Fatal("expected ok")
	}
	if s.Min != 60 || s.Max != 200 || s.Avg != 122 {
		t.Errorf("min/max/avg = %v/%v/%v", s.Min, s.Max, s.Avg)
	}
	// 70, 100, 180 are inside (bounds inclusive): 3 of 5.
	if s.TIR != 60 || s.Below != 20 || s.Above != 20 {
		t.Errorf("tir/below/above = %v/%v/%v", s.TIR, s.Below, s.Above)
	}
	if s.Start != 60 || s.End != 200 {
		t.Errorf("start/end = %v/%v", s.Start, s.End)
	}
}

func TestSummarizeDerivedStats(t *testing.T) {
	s, ok := Summarize(samples(60, 70, 100, 180, 200), DefaultRange)
	if !ok {
		t.Fatal("expected ok")
	}
	// avg=122; deviations -62,-52,-22,58,78; population variance 3296, stddev ~57.41.
	if want := 57.41; math.Abs(s.StdDev-want) > 0.01 {
		t.Errorf("stddev = %v, want ~%v", s.StdDev, want)
	}
	if want := 47.06; math.Abs(s.CV-want) > 0.01 {
		t.Errorf("cv = %v, want ~%v", s.CV, want)
	}
	// GMI = 3.31 + 0.02392*122.
	if want := 6.228; math.Abs(s.GMI-want) > 0.001 {
		t.Errorf("gmi = %v, want ~%v", s.GMI, want)
	}
	if s.VeryLow != 0 || s.VeryHigh != 0 {
		t.Errorf("verylow/veryhigh = %v/%v, want 0/0 (no sample crosses 54 or 250)", s.VeryLow, s.VeryHigh)
	}
}

func TestSummarizeVeryLowAndVeryHigh(t *testing.T) {
	// 40 is below the clinical 54 mg/dL threshold; 300 is above 250; 100 is neither.
	s, _ := Summarize(samples(40, 100, 300), DefaultRange)
	if math.Abs(s.VeryLow-100.0/3) > 0.0001 {
		t.Errorf("verylow = %v, want ~%v", s.VeryLow, 100.0/3)
	}
	if math.Abs(s.VeryHigh-100.0/3) > 0.0001 {
		t.Errorf("veryhigh = %v, want ~%v", s.VeryHigh, 100.0/3)
	}
}

func TestSummarizeUnsorted(t *testing.T) {
	in := samples(100, 150)
	in[0], in[1] = in[1], in[0]
	s, _ := Summarize(in, DefaultRange)
	if s.Start != 100 || s.End != 150 {
		t.Errorf("start/end = %v/%v, want 100/150", s.Start, s.End)
	}
}

func TestSparkline(t *testing.T) {
	if got := Sparkline(samples(1, 2, 3, 4, 5, 6, 7, 8), 8); got != "▁▂▃▄▅▆▇█" {
		t.Errorf("got %q", got)
	}
	if got := Sparkline(samples(100, 100, 100), 3); got != "▁▁▁" {
		t.Errorf("flat series got %q", got)
	}
	if got := len([]rune(Sparkline(samples(1, 2, 3, 4, 5, 6, 7, 8, 9, 10), 4))); got != 4 {
		t.Errorf("width = %d, want 4", got)
	}
	if Sparkline(nil, 5) != "" {
		t.Error("empty input should give empty string")
	}
}
