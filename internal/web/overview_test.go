package web

import (
	"context"
	"encoding/json"
	"html"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
)

var testNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// seedOverview stores seven days of readings ending at testNow with a
// nightly low, an afternoon high and one morning run, and returns the run.
func seedOverview(t *testing.T, e *env) {
	t.Helper()
	ctx := context.Background()
	var samples []stats.Sample
	for d := 7; d >= 0; d-- {
		day := time.Date(2026, 9, 30-d, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 288; i++ {
			at := day.Add(time.Duration(i) * 5 * time.Minute)
			if at.After(testNow) {
				break
			}
			v := 115 + 30*math.Sin(float64(i)/288*2*math.Pi)
			switch {
			case d == 2 && i >= 36 && i < 42: // 03:00-03:30, a low
				v = 55
			case i >= 168 && i < 176: // 14:00-14:40, a high
				v = 260
			}
			samples = append(samples, stats.Sample{Time: at, Value: v})
		}
	}
	if err := e.srv.Store.SaveSamples(ctx, "dexcom", samples); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	var inRun []stats.Sample
	for _, s := range samples {
		if !s.Time.Before(start) && s.Time.Before(start.Add(45*time.Minute)) {
			inRun = append(inRun, s)
		}
	}
	sum, _ := stats.Summarize(inRun, stats.Range{Low: 70, High: 180})
	act := &jobs.Activity{StravaID: "9001", Name: "Morning Run", Sport: "Run", Start: start, Duration: 45 * time.Minute,
		Status: jobs.StatusDone, Summary: &sum}
	if err := e.srv.Store.SaveActivity(ctx, act); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewCardRegistryHasARendererForEveryCard(t *testing.T) {
	t.Parallel()
	for _, def := range store.OverviewCards {
		if overviewCardRenderers[def.ID] == nil {
			t.Errorf("card %q has no renderer", def.ID)
		}
	}
	for id := range overviewCardRenderers {
		if _, ok := store.OverviewCardDefByID(id); !ok {
			t.Errorf("renderer %q is not a registered card", id)
		}
	}
	for id := range needsReadings {
		if _, ok := store.OverviewCardDefByID(id); !ok {
			t.Errorf("needsReadings lists unknown card %q", id)
		}
	}
}

func TestOverviewPageRendersEveryCard(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.srv.Now = func() time.Time { return testNow }
	c := e.login(t)
	seedOverview(t, e)

	w := e.get(t, "/stats?range=7d", c)
	if w.Code != 200 {
		t.Fatalf("GET /stats = %d", w.Code)
	}
	body := w.Body.String()
	for _, def := range store.OverviewCards {
		if !strings.Contains(body, `id="ov-`+def.ID+`"`) {
			t.Errorf("card %s missing from the page", def.ID)
		}
	}
	for _, chart := range []string{"ov-tir-all", "ov-tir-act", "ov-agp", "ov-trend-glucose", "ov-trend-tir", "ov-heatmap", "ov-calendar", "ov-dayparts", "ov-bysport"} {
		if !strings.Contains(body, `<gv-chart id="`+chart+`"`) {
			t.Errorf("chart %s missing", chart)
		}
	}
	for _, want := range []string{"Key numbers", "Morning Run", "Lows and highs", "Activity insights", "Very low"} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	// The low at 03:00 two days ago is listed, and no compare deltas are shown without compare.
	if strings.Contains(body, " pts vs previous") {
		t.Error("deltas shown without ?compare=prev")
	}
}

func TestOverviewCompareShowsDeltas(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.srv.Now = func() time.Time { return testNow }
	c := e.login(t)
	seedOverview(t, e)

	body := e.get(t, "/stats?range=7d&compare=prev", c).Body.String()
	// The previous 7 days hold one day of data at most, so tiles compare.
	if !strings.Contains(body, "changes compare with") {
		t.Error("compare subtitle missing")
	}
	// "all" never compares.
	if body := e.get(t, "/stats?range=all&compare=prev", c).Body.String(); strings.Contains(body, "changes compare with") {
		t.Error("all compared")
	}
}

func TestOverviewCustomRangeAndBadRange(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.srv.Now = func() time.Time { return testNow }
	c := e.login(t)
	seedOverview(t, e)

	w := e.get(t, "/stats?from=2026-09-25&to=2026-09-27", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "25 Sep – 28 Sep") {
		t.Errorf("custom range = %d, body has label: %v", w.Code, strings.Contains(w.Body.String(), "25 Sep"))
	}
	bad := e.get(t, "/stats?from=2026-09-27&to=2026-09-25", c)
	if bad.Code != 200 || !strings.Contains(bad.Body.String(), "before the start date") {
		t.Errorf("bad range not explained: %d", bad.Code)
	}
}

func TestOverviewEmptyData(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	body := e.get(t, "/stats", c).Body.String()
	if !strings.Contains(body, "No glucose readings in this range") {
		t.Error("empty state missing")
	}
	if strings.Contains(body, `id="ov-agp"`) || strings.Contains(body, `id="ov-kpis"`) {
		t.Error("reading-based cards rendered without readings")
	}
	if !strings.Contains(body, `id="ov-by_sport"`) || !strings.Contains(body, `id="ov-table"`) {
		t.Error("activity cards should still render")
	}
}

func TestOverviewLayoutControlsCardsAndOrder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.srv.Now = func() time.Time { return testNow }
	c := e.login(t)
	seedOverview(t, e)

	body := strings.TrimSuffix(validSettings, "}") +
		`,"overviewOrder":"table,heatmap,kpis","overviewOn":{"table":true,"heatmap":true,"kpis":false},` +
		`"overviewOpt":{"heatmap_metric":"tir"},"overviewRange":"14d"}`
	if w := e.action("/actions/settings", body, c, nil); !strings.Contains(w.Body.String(), "Settings saved") {
		t.Fatalf("save: %s", w.Body)
	}
	cfg, _ := e.srv.Store.LoadConfig()
	cards := cfg.OverviewCards()
	if cards[0].ID != "table" || cards[1].ID != "heatmap" || cards[1].Options["metric"] != "tir" || cards[2].ID != "kpis" || cards[2].Enabled {
		t.Errorf("stored layout = %+v", cards[:3])
	}
	if len(cards) != len(store.OverviewCards) {
		t.Errorf("cards not completed: %d", len(cards))
	}
	if cfg.OverviewRange() != "14d" {
		t.Errorf("default range = %q", cfg.OverviewRange())
	}

	page := e.get(t, "/stats", c).Body.String()
	if strings.Contains(page, `id="ov-kpis"`) {
		t.Error("disabled card rendered")
	}
	iTable, iHeat := strings.Index(page, `id="ov-table"`), strings.Index(page, `id="ov-heatmap"`)
	if iTable < 0 || iHeat < 0 || iTable > iHeat {
		t.Errorf("order not respected: table at %d, heatmap at %d", iTable, iHeat)
	}
	if !strings.Contains(page, "Time in range for each hour of the week") {
		t.Error("heatmap option metric=tir not applied")
	}
	if !strings.Contains(page, "14 days") { // configured default range highlighted via its label
		t.Error("default range not used")
	}

	// A form that does not carry the layout leaves it alone.
	e.action("/actions/settings", validSettings, c, nil)
	if cfg2, _ := e.srv.Store.LoadConfig(); cfg2.OverviewCards()[0].ID != "table" {
		t.Error("a save without layout signals reset the layout")
	}

	// A bad default range is rejected.
	bad := strings.TrimSuffix(validSettings, "}") + `,"overviewRange":"5y"}`
	if w := e.action("/actions/settings", bad, c, nil); !strings.Contains(w.Body.String(), `data-variant="error"`) {
		t.Errorf("bad range accepted: %s", w.Body)
	}
}

func TestSettingsPageListsOverviewCards(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	page := e.get(t, "/settings", c).Body.String()
	for _, def := range store.OverviewCards {
		if !strings.Contains(page, `data-bind="overviewOn.`+def.ID+`"`) || !strings.Contains(page, def.Title) {
			t.Errorf("settings page has no toggle for %s", def.ID)
		}
	}
	for _, want := range []string{`data-bind="overviewOpt.heatmap_metric"`, `data-bind="overviewOpt.agp_hours"`, `data-bind="overviewRange"`, "$overviewOrder"} {
		if !strings.Contains(page, want) {
			t.Errorf("settings page is missing %s", want)
		}
	}
	// The initial signals carry every card.
	if !strings.Contains(html.UnescapeString(page), `"overviewOrder":"kpis,tir,agp,trend,heatmap,calendar,dayparts,episodes,by_sport,insights,table,sources"`) {
		t.Error("initial overviewOrder signal missing or in the wrong order")
	}
}

func TestOverviewSignalsRoundTrip(t *testing.T) {
	t.Parallel()
	cfg := store.Config{OverviewLayout: store.EncodeOverviewLayout([]store.OverviewCard{
		{ID: "agp", Enabled: true, Options: map[string]string{"hours": "rest"}},
		{ID: "kpis", Enabled: false},
	}), OverviewDefaultRange: "90d"}
	sig := overviewSignals(cfg)
	raw, _ := json.Marshal(sig)
	var v settingsSignals
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	got, err := store.ParseOverviewLayout(v.overviewLayout())
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != "agp" || got[0].Options["hours"] != "rest" || got[1].ID != "kpis" || got[1].Enabled {
		t.Errorf("round trip = %+v", got[:2])
	}
	if v.OverviewRange != "90d" {
		t.Errorf("range = %q", v.OverviewRange)
	}
}

func TestBuildOverviewModel(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 2)
	var s []stats.Sample
	for at := from; at.Before(to); at = at.Add(5 * time.Minute) {
		v := 110.0
		if at.Hour() == 3 && at.Day() == 1 { // an hour-long low on day one
			v = 50
		}
		s = append(s, stats.Sample{Time: at, Value: v})
	}
	run := jobs.Activity{StravaID: "1", Start: from.Add(8 * time.Hour), Duration: time.Hour, Status: jobs.StatusDone}
	thr := analytics.Thresholds{VeryLow: 54, Low: 70, High: 180, VeryHigh: 250}
	m := buildOverviewModel(overviewInput{From: from, To: to, Samples: s, Acts: []jobs.Activity{run}, Thr: thr, Loc: loc})
	if !m.HasData || len(m.Daily) != 2 || m.Cur.Days != 2 {
		t.Fatalf("model = %+v", m.Cur)
	}
	if len(m.Windows) != 1 || m.TIRDuring.Count != 12 {
		t.Errorf("windows %d, during %d readings, want 1 and 12", len(m.Windows), m.TIRDuring.Count)
	}
	if m.TIRRest.Count+m.TIRDuring.Count != m.Cur.Count {
		t.Error("during + outside must add up to all readings")
	}
	if got := analytics.SummarizeEpisodes(analytics.OfKind(m.Episodes, analytics.KindVeryLow)); got.Count != 1 || got.Nocturnal != 1 {
		t.Errorf("very-low episodes = %+v", got)
	}
	if m.Prev != nil {
		t.Error("Prev set without compare")
	}
	if empty := buildOverviewModel(overviewInput{From: from, To: to, Thr: thr, Loc: loc}); empty.HasData || len(empty.Daily) != 0 {
		t.Error("empty input reports data")
	}
	cmp := buildOverviewModel(overviewInput{From: from, To: to, Samples: s, Compare: true, PrevFrom: from.AddDate(0, 0, -2), PrevTo: from, Thr: thr, Loc: loc})
	if cmp.Prev == nil || cmp.Prev.HasData {
		t.Errorf("Prev = %+v, want a period without data", cmp.Prev)
	}
}

func TestOverviewCacheEvictsAndKeysOnVersion(t *testing.T) {
	t.Parallel()
	var c overviewCache
	k := overviewKey{Range: "7d", Loc: "UTC"}
	if _, ok := c.get(k); ok {
		t.Fatal("empty cache hit")
	}
	m := &overviewModel{HasData: true}
	c.put(k, m)
	if got, ok := c.get(k); !ok || got != m {
		t.Fatal("miss after put")
	}
	k2 := k
	k2.Ver.Samples = 1 // new data: a different key
	if _, ok := c.get(k2); ok {
		t.Error("stale hit across data versions")
	}
	for i := 0; i < overviewCacheMax+2; i++ {
		c.put(overviewKey{Range: "x", From: int64(i)}, m)
	}
	if len(c.m) > overviewCacheMax {
		t.Errorf("cache grew to %d", len(c.m))
	}
}

func TestNearbyActivityAndRecentEpisodes(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	acts := []jobs.Activity{{StravaID: "1", Start: start, Duration: time.Hour}}
	during := analytics.Episode{Kind: analytics.KindLow, Start: start.Add(30 * time.Minute)}
	after := analytics.Episode{Kind: analytics.KindLow, Start: start.Add(3 * time.Hour)}
	late := analytics.Episode{Kind: analytics.KindLow, Start: start.Add(6 * time.Hour)}
	for name, tc := range map[string]struct {
		e    analytics.Episode
		want bool
	}{"during": {during, true}, "within 4h after": {after, true}, "much later": {late, false}} {
		if _, ok := nearbyActivity(acts, tc.e); ok != tc.want {
			t.Errorf("%s: found = %v", name, ok)
		}
	}
	eps := []analytics.Episode{
		{Kind: analytics.KindVeryLow, Start: start}, {Kind: analytics.KindLow, Start: start},
		{Kind: analytics.KindHigh, Start: start.Add(time.Hour)}, {Kind: analytics.KindVeryHigh, Start: start.Add(time.Hour)},
	}
	got := recentEpisodes(eps, 10)
	if len(got) != 2 || got[0].Kind != analytics.KindHigh {
		t.Errorf("recent = %+v, want the high first and the nested very-* kinds left out", got)
	}
}
