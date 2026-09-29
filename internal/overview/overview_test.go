package overview

import (
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/stats"
)

var utc = time.UTC

func act(id, sport string, start time.Time, status string, sum *stats.Summary) jobs.Activity {
	return jobs.Activity{StravaID: id, Sport: sport, Start: start, Status: status, Summary: sum}
}

func sum(tir, avg, cv float64) *stats.Summary {
	return &stats.Summary{TIR: tir, Avg: avg, CV: cv}
}

func TestBuildIgnoresActivitiesWithoutASummary(t *testing.T) {
	acts := []jobs.Activity{
		act("1", "Run", time.Date(2026, 9, 20, 8, 0, 0, 0, utc), jobs.StatusFailed, nil),
		act("2", "Run", time.Date(2026, 9, 20, 9, 0, 0, 0, utc), jobs.StatusProcessing, sum(90, 110, 20)),
		act("3", "Run", time.Date(2026, 9, 20, 10, 0, 0, 0, utc), jobs.StatusPending, nil),
	}
	d := Build(acts, utc)
	if len(d.Trend) != 0 || len(d.BySport) != 0 {
		t.Errorf("Trend=%v BySport=%v, want both empty (nothing is StatusDone)", d.Trend, d.BySport)
	}
	if len(d.Activities) != 3 {
		t.Errorf("Activities = %d, want 3 (the raw table shows every status)", len(d.Activities))
	}
}

func TestBuildAveragesSameDayActivities(t *testing.T) {
	day := time.Date(2026, 9, 20, 0, 0, 0, 0, utc)
	acts := []jobs.Activity{
		act("1", "Run", day.Add(8*time.Hour), jobs.StatusDone, sum(100, 100, 10)),
		act("2", "Run", day.Add(18*time.Hour), jobs.StatusDone, sum(80, 120, 30)),
	}
	d := Build(acts, utc)
	if len(d.Trend) != 1 {
		t.Fatalf("Trend = %+v, want 1 point", d.Trend)
	}
	p := d.Trend[0]
	if !p.Day.Equal(day) || p.Count != 2 || p.TIR != 90 || p.Avg != 110 || p.CV != 20 {
		t.Errorf("point = %+v, want day=%v count=2 TIR=90 Avg=110 CV=20", p, day)
	}
}

func TestBuildSeparatesDifferentDaysChronologically(t *testing.T) {
	day1 := time.Date(2026, 9, 19, 0, 0, 0, 0, utc)
	day2 := time.Date(2026, 9, 20, 0, 0, 0, 0, utc)
	acts := []jobs.Activity{
		act("1", "Run", day2.Add(time.Hour), jobs.StatusDone, sum(90, 100, 10)),
		act("2", "Run", day1.Add(time.Hour), jobs.StatusDone, sum(80, 100, 10)),
	}
	d := Build(acts, utc)
	if len(d.Trend) != 2 || !d.Trend[0].Day.Equal(day1) || !d.Trend[1].Day.Equal(day2) {
		t.Fatalf("Trend = %+v, want [day1, day2] in order", d.Trend)
	}
}

func TestBuildBucketsByLocalDayNotUTC(t *testing.T) {
	// 23:30 in UTC+2 is already the next local day.
	loc := time.FixedZone("UTC+2", 2*60*60)
	start := time.Date(2026, 9, 19, 23, 30, 0, 0, loc)
	d := Build([]jobs.Activity{act("1", "Run", start, jobs.StatusDone, sum(90, 100, 10))}, loc)
	want := time.Date(2026, 9, 19, 0, 0, 0, 0, loc)
	if len(d.Trend) != 1 || !d.Trend[0].Day.Equal(want) {
		t.Fatalf("Trend = %+v, want a single point on %v", d.Trend, want)
	}
}

func TestBuildEmptySportFallsBackToOther(t *testing.T) {
	d := Build([]jobs.Activity{act("1", "", time.Now(), jobs.StatusDone, sum(90, 100, 10))}, utc)
	if len(d.BySport) != 1 || d.BySport[0].Sport != "Other" {
		t.Fatalf("BySport = %+v, want one entry labelled Other", d.BySport)
	}
}

func TestBuildBySportAveragesAndSortsByCountDesc(t *testing.T) {
	now := time.Now()
	acts := []jobs.Activity{
		act("1", "Run", now, jobs.StatusDone, sum(90, 100, 10)),
		act("2", "Run", now, jobs.StatusDone, sum(70, 140, 10)),
		act("3", "Ride", now, jobs.StatusDone, sum(100, 120, 10)),
	}
	d := Build(acts, utc)
	if len(d.BySport) != 2 {
		t.Fatalf("BySport = %+v, want 2 sports", d.BySport)
	}
	run := d.BySport[0]
	if run.Sport != "Run" || run.Count != 2 || run.AvgTIR != 80 || run.AvgGlucose != 120 {
		t.Errorf("Run = %+v, want count=2 AvgTIR=80 AvgGlucose=120 (Run has more activities, so it sorts first)", run)
	}
	ride := d.BySport[1]
	if ride.Sport != "Ride" || ride.Count != 1 {
		t.Errorf("Ride = %+v, want count=1", ride)
	}
}

func TestBuildBySportTiesBreakAlphabetically(t *testing.T) {
	now := time.Now()
	acts := []jobs.Activity{
		act("1", "Swim", now, jobs.StatusDone, sum(90, 100, 10)),
		act("2", "Ride", now, jobs.StatusDone, sum(90, 100, 10)),
	}
	d := Build(acts, utc)
	if len(d.BySport) != 2 || d.BySport[0].Sport != "Ride" || d.BySport[1].Sport != "Swim" {
		t.Fatalf("BySport = %+v, want [Ride, Swim] (equal counts, alphabetical)", d.BySport)
	}
}

func TestBuildEmptyInput(t *testing.T) {
	d := Build(nil, utc)
	if len(d.Trend) != 0 {
		t.Errorf("Trend = %v, want empty", d.Trend)
	}
	if len(d.BySport) != 0 {
		t.Errorf("BySport = %v, want empty", d.BySport)
	}
	if len(d.Activities) != 0 {
		t.Errorf("Activities = %v, want empty", d.Activities)
	}
}
