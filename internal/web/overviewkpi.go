package web

import (
	"fmt"
	"math"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// This file holds the headline numbers of one period and the change against
// the previous one. They are small local helpers; internal/analytics may grow
// its own KPI/delta types, in which case these are the only two functions to
// swap (computeKPIs and kpiDelta).

// cvTarget is the consensus ceiling for glucose variability (CV, percent).
const cvTarget = 36.0

// periodKPIs are the numbers of the key-numbers card for one window.
type periodKPIs struct {
	HasData  bool
	Count    int
	Avg      float64 // mg/dL
	CV       float64 // percent
	GMI      float64 // percent
	TIR      analytics.TIR5
	GRI      analytics.GRI
	Coverage analytics.Coverage
	Days     int // local calendar days with at least one reading
}

// computeKPIs summarizes samples over [from, to].
func computeKPIs(samples []stats.Sample, from, to time.Time, th analytics.Thresholds, loc *time.Location) periodKPIs {
	k := periodKPIs{}
	k.Coverage = analytics.ComputeCoverage(samples, from, to, 0, 0)
	if len(samples) == 0 {
		return k
	}
	b := analytics.Aggregate(samples, th)
	k.HasData, k.Count = true, b.Count
	k.Avg, k.CV, k.TIR = b.Avg, b.CV, b.TIR
	k.GMI = 3.31 + 0.02392*b.Avg // ADA/ATTD consensus, same as stats.Summarize
	k.GRI = analytics.ComputeGRI(b.TIR)
	k.Days = len(analytics.ComputeDaily(samples, th, loc))
	return k
}

// kpiDelta describes cur against prev for a stat tile. better is +1 when a
// higher value is good news (time in range), -1 when a lower one is (CV,
// risk index) and 0 for neutral numbers such as the average. unit is the
// suffix on the difference ("pts", "%"); scale converts a raw difference to
// the displayed one (mg/dL to mmol/L for glucose; 1 otherwise).
func kpiDelta(cur, prev float64, better int, unit string, scale float64, decimals int) *Delta {
	diff := (cur - prev) * scale
	eps := math.Pow(10, -float64(decimals)) / 2
	if math.Abs(diff) < eps {
		return &Delta{Text: "no change vs previous", Dir: "flat"}
	}
	d := &Delta{Dir: "up"}
	sign := "+"
	if diff < 0 {
		d.Dir, sign = "down", "−"
	}
	d.Text = fmt.Sprintf("%s%.*f %s vs previous", sign, decimals, math.Abs(diff), unit)
	if better != 0 {
		if (diff > 0) == (better > 0) {
			d.Tone = "good"
		} else {
			d.Tone = "bad"
		}
	}
	return d
}
