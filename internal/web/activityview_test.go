package web

import (
	"bytes"
	"strings"
	"testing"
	"time"

	g "maragu.dev/gomponents"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
)

func renderNode(t *testing.T, n g.Node) string {
	t.Helper()
	var b bytes.Buffer
	if err := n.Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

var viewT0 = time.Date(2026, 9, 30, 7, 0, 0, 0, time.UTC)

func viewAct(id, sport string, dayOffset int, tir float64) jobs.Activity {
	return jobs.Activity{
		StravaID: id, Name: "Act " + id, Sport: sport, Status: jobs.StatusDone,
		Start: viewT0.AddDate(0, 0, dayOffset), Duration: 40 * time.Minute,
		Summary: &stats.Summary{Count: 8, TIR: tir},
	}
}

func TestActivityNeighboursAndRank(t *testing.T) {
	all := []jobs.Activity{
		viewAct("1", "Run", -5, 50), viewAct("2", "Run", -4, 60), viewAct("3", "Ride", -3, 99),
		viewAct("4", "Run", -2, 70), viewAct("5", "Run", -1, 80), viewAct("6", "Run", 0, 90),
		viewAct("7", "Run", 1, 55),
	}
	// Deliberately unsorted input.
	all[0], all[6] = all[6], all[0]
	cur := viewAct("6", "Run", 0, 90)
	prev, next, rank := activityNeighbours(all, cur)
	if prev == nil || prev.ID != "5" {
		t.Errorf("prev = %+v, want 5", prev)
	}
	if next == nil || next.ID != "7" {
		t.Errorf("next = %+v, want 7", next)
	}
	// Five other runs (the ride does not count): 50 60 70 80 55 — all below 90.
	if rank == nil || rank.Others != 5 || rank.Percent != 100 {
		t.Errorf("rank = %+v, want 100%% of 5", rank)
	}

	first := viewAct("1", "Run", -5, 50)
	prev, _, rank = activityNeighbours(all, first)
	if prev != nil {
		t.Errorf("oldest activity has prev %+v", prev)
	}
	if rank == nil || rank.Percent != 0 {
		t.Errorf("worst run rank = %+v, want 0%%", rank)
	}

	// Too few others of the sport: no rank at all.
	_, _, rank = activityNeighbours(all[:3], viewAct("3", "Ride", -3, 99))
	if rank != nil {
		t.Errorf("rank with a single ride = %+v, want nil", rank)
	}
}

func viewData() ActivityData {
	act := viewAct("6", "Run", 0, 90)
	act.Distance, act.ElevationGain = 8000, 120
	act.Summary = &stats.Summary{Count: 8, TIR: 90, Below: 4, Above: 6, Avg: 110, Min: 80, Max: 160,
		Start: 120, End: 95, CV: 22, StdDev: 24, GMI: 5.9, VeryLow: 1, VeryHigh: 2}
	act.HeartRate = nil
	var samples []stats.Sample
	for i := 0; i < 20; i++ {
		samples = append(samples, stats.Sample{Time: act.Start.Add(time.Duration(i-3) * 5 * time.Minute), Value: 120 - float64(i)})
	}
	cfg := store.Config{Unit: "mg/dL", RangeLow: 70, RangeHigh: 180, VeryLow: 54, VeryHigh: 250, PreMin: 15, PostMin: 15}
	thr := analytics.FromRange(cfg.Range())
	in := analytics.InsightsFor(samples, []analytics.ActivityInput{{ID: "6", Sport: "Run", Start: act.Start, End: act.End()}}, thr, time.UTC)
	d := ActivityData{
		Act: act, Samples: samples, Cfg: cfg, Loc: time.UTC, Now: viewT0.Add(time.Hour), Thr: thr,
		Prev: &ActivityRef{ID: "5", Name: "Act 5", Start: viewT0.AddDate(0, 0, -1)},
		Rank: &SportRank{Sport: "Run", Percent: 80, Others: 6},
	}
	if len(in) > 0 && in[0].HasData {
		d.Insight = &in[0]
	}
	return d
}

func TestActivityBodyShowsChartTilesAndAround(t *testing.T) {
	out := renderNode(t, ActivityBody(viewData()))
	for _, want := range []string{
		`<gv-chart id="activity-chart"`, "<noscript>", "Variability (CV)", "GMI", "Very low / very high",
		"Duration", "Distance", "5:00 /km", "Elevation gain", "Rank among Run", "Before, during and after", "Drop rate",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("activity body is missing %q", want)
		}
	}
}

func TestActivityBodyWithoutReadings(t *testing.T) {
	d := viewData()
	d.Samples, d.Insight = nil, nil
	out := renderNode(t, ActivityBody(d))
	if strings.Contains(out, "gv-chart") {
		t.Error("no chart without readings")
	}
	if !strings.Contains(out, "No glucose readings for this activity") || strings.Contains(out, "Before, during and after") {
		t.Errorf("empty state wrong: %s", out)
	}
}

func TestActivityNavDisablesMissingNeighbour(t *testing.T) {
	out := renderNode(t, g.Group(activityNav(viewData())))
	if !strings.Contains(out, `href="/activity/5"`) || !strings.Contains(out, "Older") {
		t.Errorf("older link missing: %s", out)
	}
	if !strings.Contains(out, `aria-disabled="true"`) {
		t.Errorf("missing newer neighbour should render disabled: %s", out)
	}
}

func dashView(cur float64, ageMin int, rising bool) DashData {
	now := viewT0.Add(5 * time.Hour)
	var day []stats.Sample
	for i := 0; i < 24*12; i++ { // 24 h of 5-minute readings ending ageMin ago
		ts := now.Add(-time.Duration(ageMin)*time.Minute - time.Duration(24*12-1-i)*5*time.Minute)
		v := cur
		if rising {
			v = cur - float64(24*12-1-i)*10
			if v < 60 {
				v = 60
			}
		}
		day = append(day, stats.Sample{Time: ts, Value: v})
	}
	thr := analytics.FromRange(stats.Range{Low: 70, High: 180})
	return DashData{Unit: render.MgDL, Loc: time.UTC, Now: now, Day: day, DayTIR: analytics.ComputeTIR5(day, thr), Thr: thr}
}

func TestNowCard(t *testing.T) {
	out := renderNode(t, nowCard(dashView(140, 2, true)))
	for _, want := range []string{`id="now"`, "Current glucose", "140", "↑", `id="dash-3h"`, `id="dash-tir24"`} {
		if !strings.Contains(out, want) {
			t.Errorf("now card is missing %q", want)
		}
	}
	stale := renderNode(t, nowCard(dashView(140, 45, false)))
	if !strings.Contains(stale, "Out of date") {
		t.Error("a reading 45 minutes old should be marked out of date")
	}
	empty := renderNode(t, nowCard(DashData{Unit: render.MgDL, Loc: time.UTC, Now: viewT0}))
	if !strings.Contains(empty, "No glucose readings yet") || strings.Contains(empty, "gv-chart") {
		t.Errorf("empty now card: %s", empty)
	}
}
