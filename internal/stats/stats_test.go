package stats

import (
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
