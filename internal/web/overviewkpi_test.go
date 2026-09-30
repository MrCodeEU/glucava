package web

import (
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/stats"
)

func TestKPIDelta(t *testing.T) {
	d := kpiDelta(82, 78, 1, "pts", 1, 1)
	if d.Dir != "up" || d.Tone != "good" || d.Text != "+4.0 pts vs previous" {
		t.Errorf("TIR up = %+v", d)
	}
	d = kpiDelta(40, 30, -1, "%", 1, 1)
	if d.Dir != "up" || d.Tone != "bad" {
		t.Errorf("CV up must be bad: %+v", d)
	}
	d = kpiDelta(120, 130, 0, "mg/dL", 1, 0)
	if d.Dir != "down" || d.Tone != "" || d.Text != "−10 mg/dL vs previous" {
		t.Errorf("neutral avg down = %+v", d)
	}
	d = kpiDelta(150, 150.01, 1, "pts", 1, 1)
	if d.Dir != "flat" || d.Tone != "" {
		t.Errorf("flat = %+v", d)
	}
	// mmol/L scale: 18.016 mg/dL is one mmol/L.
	d = kpiDelta(118.016, 100, 0, "mmol/L", 1/18.016, 1)
	if d.Text != "+1.0 mmol/L vs previous" {
		t.Errorf("scaled = %+v", d)
	}
}

func TestComputeKPIs(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	to := from.Add(24 * time.Hour)
	var s []stats.Sample
	for i := 0; i < 288; i++ {
		v := 100.0
		if i < 29 { // ten percent low
			v = 60
		}
		s = append(s, stats.Sample{Time: from.Add(time.Duration(i) * 5 * time.Minute), Value: v})
	}
	k := computeKPIs(s, from, to, analytics.Thresholds{VeryLow: 54, Low: 70, High: 180, VeryHigh: 250}, loc)
	if !k.HasData || k.Count != 288 || k.Days != 1 {
		t.Fatalf("kpis = %+v", k)
	}
	if k.TIR.Low < 10 || k.TIR.Low > 10.2 || k.TIR.InRange < 89.8 {
		t.Errorf("TIR = %+v", k.TIR)
	}
	if k.Coverage.Pct < 99 {
		t.Errorf("coverage = %v", k.Coverage.Pct)
	}
	if k.GMI < 5.6 || k.GMI > 5.8 {
		t.Errorf("GMI = %v", k.GMI)
	}
	if empty := computeKPIs(nil, from, to, analytics.Thresholds{}, loc); empty.HasData {
		t.Error("empty period reports data")
	}
}
