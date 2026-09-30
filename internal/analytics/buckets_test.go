package analytics

import (
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

func TestAggregateMatchesSummarize(t *testing.T) {
	s := series(base, 5*time.Minute, 100, 120, 180, 60, 200, 250, 300, 90)
	got := Aggregate(s, th)
	want, _ := stats.Summarize(s, stats.Range{Low: 70, High: 180})
	if got.Count != want.Count || !near(got.Avg, want.Avg) || !near(got.StdDev, want.StdDev) || !near(got.CV, want.CV) ||
		got.Min != want.Min || got.Max != want.Max || !near(got.TIR.InRange, want.TIR) {
		t.Errorf("aggregate %+v vs summary %+v", got, want)
	}
	if z := Aggregate(nil, th); z != (Block{}) {
		t.Errorf("empty = %+v", z)
	}
}

func TestDayParts(t *testing.T) {
	// One reading at each local hour 03, 09, 15, 21 in Vienna (UTC+2 in Sept).
	var s []stats.Sample
	for i, h := range []int{3, 9, 15, 21} {
		s = append(s, stats.Sample{Time: time.Date(2026, 9, 20, h, 0, 0, 0, vie), Value: float64(100 * (i + 1))})
	}
	parts := ComputeDayParts(s, th, vie)
	for i, p := range parts {
		if p.Count != 1 || p.Avg != float64(100*(i+1)) {
			t.Errorf("part %s = %+v", p.Name, p.Block)
		}
	}
	if parts[0].Name != "Night" || parts[3].ToHour != 24 {
		t.Errorf("bounds = %+v %+v", parts[0], parts[3])
	}
	// Edge: 05:59 night, 06:00 morning, 23:59 evening.
	edge := []stats.Sample{
		{Time: time.Date(2026, 9, 20, 5, 59, 0, 0, vie), Value: 1},
		{Time: time.Date(2026, 9, 20, 6, 0, 0, 0, vie), Value: 1},
		{Time: time.Date(2026, 9, 20, 23, 59, 0, 0, vie), Value: 1},
	}
	p := ComputeDayParts(edge, th, vie)
	if p[0].Count != 1 || p[1].Count != 1 || p[3].Count != 1 || p[2].Count != 0 {
		t.Errorf("edges = %d %d %d %d", p[0].Count, p[1].Count, p[2].Count, p[3].Count)
	}
}

func TestWeekdayHour(t *testing.T) {
	// 2026-09-20 is a Sunday.
	s := []stats.Sample{
		{Time: time.Date(2026, 9, 20, 8, 5, 0, 0, vie), Value: 100},
		{Time: time.Date(2026, 9, 20, 8, 50, 0, 0, vie), Value: 200},
		{Time: time.Date(2026, 9, 20, 8, 55, 0, 0, vie), Value: 60},
		{Time: time.Date(2026, 9, 21, 0, 0, 0, 0, vie), Value: 100},
	}
	g := ComputeWeekdayHour(s, th, vie)
	c := g[time.Sunday][8]
	if c.Count != 3 || !near(c.Mean, 120) || !near(c.InRange, 100.0/3) || !near(c.Below, 100.0/3) || !near(c.Above, 100.0/3) {
		t.Errorf("sunday 8 = %+v", c)
	}
	if g[time.Monday][0].Count != 1 || g[time.Tuesday][0].Count != 0 {
		t.Errorf("monday 0 = %+v", g[time.Monday][0])
	}
	// Same instant lands in a different cell in UTC vs Vienna.
	if u := ComputeWeekdayHour(s, th, nil); u[time.Sunday][6].Count != 3 {
		t.Errorf("utc = %+v", u[time.Sunday][6])
	}
}

func TestDailyCalendarDaysAndDST(t *testing.T) {
	// Fall-back day Oct 25 2026 in Vienna is 25 hours; must stay one day.
	start := time.Date(2026, 10, 25, 0, 0, 0, 0, vie)
	var s []stats.Sample
	for ts := start.Add(-time.Hour); ts.Before(start.Add(26 * time.Hour)); ts = ts.Add(5 * time.Minute) {
		s = append(s, stats.Sample{Time: ts, Value: 100})
	}
	days := ComputeDaily(s, th, vie)
	if len(days) != 3 {
		t.Fatalf("days = %d, want 3", len(days))
	}
	if days[1].Count != 300 || !days[1].Date.Equal(start) {
		t.Errorf("fall-back day = %d readings at %v", days[1].Count, days[1].Date)
	}
	for i := 1; i < len(days); i++ {
		if !days[i-1].Date.Before(days[i].Date) {
			t.Error("not ascending")
		}
	}
	// Spring forward: Mar 29 2026 is 23 hours.
	sp := time.Date(2026, 3, 29, 0, 0, 0, 0, vie)
	var t2 []stats.Sample
	for ts := sp; ts.Before(sp.AddDate(0, 0, 1)); ts = ts.Add(5 * time.Minute) {
		t2 = append(t2, stats.Sample{Time: ts, Value: 100})
	}
	if d := ComputeDaily(t2, th, vie); len(d) != 1 || d[0].Count != 276 {
		t.Errorf("spring day = %+v", d)
	}
	if d := ComputeDaily(nil, th, vie); len(d) != 0 {
		t.Errorf("empty = %+v", d)
	}
}

func TestDailyWeightedTIR(t *testing.T) {
	// Day 1: 1 reading in range; day 2: 3 readings, all high. Each day's TIR is
	// its own; sample weighting only matters within a day.
	s := []stats.Sample{
		{Time: time.Date(2026, 9, 20, 9, 0, 0, 0, vie), Value: 100},
		{Time: time.Date(2026, 9, 21, 9, 0, 0, 0, vie), Value: 200},
		{Time: time.Date(2026, 9, 21, 9, 5, 0, 0, vie), Value: 220},
		{Time: time.Date(2026, 9, 21, 9, 10, 0, 0, vie), Value: 100},
	}
	d := ComputeDaily(s, th, vie)
	if len(d) != 2 || d[0].TIR.InRange != 100 || !near(d[1].TIR.InRange, 100.0/3) || !near(d[1].TIR.High, 200.0/3) {
		t.Errorf("daily = %+v", d)
	}
}
