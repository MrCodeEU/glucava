package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	_ "github.com/pocketbase/pocketbase/migrations" // registers the system migrations

	"github.com/MrCodeEU/glucava/internal/chartimg"
	"github.com/MrCodeEU/glucava/internal/jobs"
	_ "github.com/MrCodeEU/glucava/internal/migrations"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/stats"
)

func newApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	return app
}

var t0 = time.Date(2026, 9, 20, 7, 0, 0, 0, time.UTC)

func TestSettingsDefaultsFromMigration(t *testing.T) {
	s := &PB{App: newApp(t)}
	set, err := s.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if set.Unit != render.MgDL || set.Range != (stats.Range{Low: 70, High: 180, VeryLow: 54, VeryHigh: 250}) || set.Post != 30*time.Minute || set.Pre != 0 || set.PollInterval != 10*time.Minute {
		t.Errorf("settings = %+v", set)
	}
	if set.PostBuffer != 5*time.Minute {
		t.Errorf("PostBuffer default = %v, want 5m (migration 013)", set.PostBuffer)
	}
	if set.DescriptionTemplate != "" {
		t.Errorf("DescriptionTemplate default = %q, want empty (migration 015)", set.DescriptionTemplate)
	}
}

// TestDescriptionTemplateRoundTrips is the store-layer half of the custom
// description template: SaveConfig must persist it and Settings (what the
// pipeline actually reads) must return the same text back.
func TestDescriptionTemplateRoundTrips(t *testing.T) {
	s := &PB{App: newApp(t)}
	c, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	c.DescriptionTemplate = "TIR {{.TIR}}% custom"
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil || got.DescriptionTemplate != c.DescriptionTemplate {
		t.Fatalf("LoadConfig = %q, %v", got.DescriptionTemplate, err)
	}
	set, err := s.Settings(context.Background())
	if err != nil || set.DescriptionTemplate != c.DescriptionTemplate {
		t.Fatalf("Settings.DescriptionTemplate = %q, %v", set.DescriptionTemplate, err)
	}
}

func TestActivityRoundTripAndUpsert(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()

	if a, err := s.Activity(ctx, "1"); err != nil || a != nil {
		t.Fatalf("unknown activity = %v, %v", a, err)
	}
	a := &jobs.Activity{StravaID: "1", Name: "Run", Start: t0, Duration: 45 * time.Minute,
		Status: jobs.StatusProcessing, Attempts: 1}
	if err := s.SaveActivity(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.Status = jobs.StatusDone
	a.Summary = &stats.Summary{Count: 3, TIR: 100, Avg: 120}
	if err := s.SaveActivity(ctx, a); err != nil {
		t.Fatal(err)
	}

	got, err := s.Activity(ctx, "1")
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	if got.Status != jobs.StatusDone || got.Summary == nil || got.Summary.TIR != 100 ||
		got.Duration != 45*time.Minute || !got.Start.Equal(t0) {
		t.Errorf("got %+v summary=%+v", got, got.Summary)
	}
	if n, _ := s.App.CountRecords("activities"); n != 1 {
		t.Errorf("activities = %d, want 1 (upsert)", n)
	}
}

func TestDeleteActivity(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()

	if err := s.DeleteActivity(ctx, "no-such-id"); err != nil {
		t.Errorf("deleting an unknown id must be a no-op: %v", err)
	}

	a := &jobs.Activity{StravaID: "1", Name: "Run", Start: t0, Duration: 45 * time.Minute, Status: jobs.StatusDone}
	if err := s.SaveActivity(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteActivity(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Activity(ctx, "1"); err != nil || got != nil {
		t.Errorf("activity = %+v, %v, want gone", got, err)
	}
	if n, _ := s.App.CountRecords("activities"); n != 0 {
		t.Errorf("activities = %d, want 0", n)
	}
}

func TestSamplesDedupeAndRange(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	in := []stats.Sample{
		{Time: t0, Value: 100},
		{Time: t0.Add(5 * time.Minute), Value: 110},
		{Time: t0.Add(10 * time.Minute), Value: 120},
	}
	for range 2 { // second save must not duplicate
		if err := s.SaveSamples(ctx, "dexcom", in); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LoadSamples(ctx, "dexcom", t0, t0.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != 100 || got[1].Value != 110 {
		t.Errorf("got %+v", got)
	}
	if other, _ := s.LoadSamples(ctx, "nightscout", t0, t0.Add(time.Hour)); len(other) != 0 {
		t.Errorf("other source returned %d samples", len(other))
	}
}

// TestLoadSamplesAnyCrossesSources is a regression test: an activity's
// window may be covered by the live source, a backfilled import under a
// different source label, or both, and all of it must be usable.
func TestLoadSamplesAnyCrossesSources(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	if err := s.SaveSamples(ctx, "dexcom", []stats.Sample{{Time: t0, Value: 100}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSamples(ctx, "glooko", []stats.Sample{{Time: t0.Add(5 * time.Minute), Value: 110}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadSamplesAny(ctx, t0, t0.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != 100 || got[1].Value != 110 {
		t.Errorf("got %+v", got)
	}
}

// TestSourceHealth is the "is my glucose source actually working" signal:
// per-source reading count and newest timestamp, so a stopped live
// connection or a failed import is visible without waiting for an activity.
func TestSourceHealth(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	if err := s.SaveSamples(ctx, "dexcom", []stats.Sample{
		{Time: t0, Value: 100}, {Time: t0.Add(5 * time.Minute), Value: 110},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSamples(ctx, "glooko", []stats.Sample{{Time: t0.Add(-time.Hour), Value: 90}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.SourceHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("SourceHealth = %+v, want 2 sources", got)
	}
	// Newest reading first: dexcom's latest (t0+5m) is after glooko's (t0-1h).
	if got[0].Source != "dexcom" || got[0].Count != 2 || !got[0].Latest.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("got[0] = %+v, want dexcom, count 2, latest t0+5m", got[0])
	}
	if got[1].Source != "glooko" || got[1].Count != 1 {
		t.Errorf("got[1] = %+v, want glooko, count 1", got[1])
	}
}

func TestRecordEvent(t *testing.T) {
	app := newApp(t)
	s := &PB{App: app}
	err := s.RecordEvent(context.Background(), jobs.Event{
		Type: jobs.EventStravaFailed, Severity: "error", Message: "x", StravaID: "9"})
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := app.FindRecordsByFilter("events", "notified = false", "", 10, 0)
	if len(recs) != 1 || recs[0].GetString("type") != jobs.EventStravaFailed {
		t.Errorf("events = %v", recs)
	}
}

func TestVault(t *testing.T) {
	app := newApp(t)
	c, err := secrets.NewCipher([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	v := &secrets.Vault{App: app, Cipher: c}

	if _, ok, err := v.Get("dexcom_password"); ok || err != nil {
		t.Fatalf("unset get = %v, %v", ok, err)
	}
	if err := v.Set("dexcom_password", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("dexcom_password", "hunter3"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := v.Get("dexcom_password")
	if err != nil || !ok || got != "hunter3" {
		t.Fatalf("get = %q, %v, %v", got, ok, err)
	}
	// Stored value must be ciphertext.
	rec, _ := app.FindFirstRecordByData("secrets", "name", "dexcom_password")
	if strings.Contains(rec.GetString("ciphertext"), "hunter") {
		t.Error("plaintext stored in database")
	}
	if n, _ := app.CountRecords("secrets"); n != 1 {
		t.Errorf("secrets rows = %d, want 1", n)
	}
	if err := v.Delete("dexcom_password"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := v.Get("dexcom_password"); ok {
		t.Error("value still present after delete")
	}
}

func TestOutbox(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	_ = s.RecordEvent(ctx, jobs.Event{Type: jobs.EventSessionExpired, Severity: "error", Message: "m"})
	_ = s.RecordEvent(ctx, jobs.Event{Type: jobs.EventStravaFailed, Severity: "error", StravaID: "5", Repaired: true})

	pending, err := s.Pending(ctx)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending = %v, %v", pending, err)
	}
	if pending[0].Created.IsZero() || pending[1].StravaID != "5" || !pending[1].Repaired {
		t.Errorf("pending = %+v", pending)
	}
	if err := s.MarkNotified(ctx, pending[0].ID); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.Pending(ctx); len(left) != 1 || left[0].ID != pending[1].ID {
		t.Errorf("left = %+v", left)
	}
}

func TestConfigNotifyAndSMTPRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	c, err := s.LoadConfig()
	if err != nil || c.NtfyURL != "" || c.EmailTo != "" || c.SMTPHost != "" {
		t.Fatalf("defaults = %+v %v", c, err)
	}
	c.NtfyURL, c.EmailTo = "https://ntfy.example/t", "me@example.com"
	c.SMTPHost, c.SMTPPort, c.SMTPUsername, c.SMTPTLS, c.SMTPSender, c.SMTPSenderName = "smtp.example.com", 465, "u", true, "g@example.com", "glucava"
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadConfig()
	if got != c {
		t.Errorf("got %+v, want %+v", got, c)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	c, err := s.LoadConfig()
	if err != nil || c.Unit != "mg/dL" || c.RangeHigh != 180 || c.PollMin != 10 {
		t.Fatalf("defaults = %+v, %v", c, err)
	}
	c.Unit, c.RangeLow, c.PostMin, c.DexcomUsername, c.NtfyURL = "mmol/L", 72, 45, "me@example.test", "https://ntfy.example/t"
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadConfig()
	if got != c {
		t.Errorf("got %+v, want %+v", got, c)
	}
	set, _ := s.Settings(context.Background())
	if set.Unit != render.MmolL || set.Range.Low != 72 || set.Post != 45*time.Minute {
		t.Errorf("Settings() did not pick up the change: %+v", set)
	}
}

// TestChartOverlayTogglesRoundTrip guards against a real bug: LoadConfig and
// SaveConfig never read or wrote chart_avg_line/chart_range_lines/
// chart_min_max/chart_hide_stats, so the four chart overlay toggles the
// Settings page shows (added in 0.3.0) silently did not persist, and
// Settings() (what the pipeline actually reads when it draws the chart
// attached to Strava) never saw them either.
func TestChartOverlayTogglesRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	c, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.ChartAvgLine || c.ChartRangeLines || c.ChartMinMax || c.ChartHideStats {
		t.Fatalf("defaults = %+v, want all four false", c)
	}
	c.ChartAvgLine, c.ChartRangeLines, c.ChartMinMax, c.ChartHideStats = true, true, true, true
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil || !got.ChartAvgLine || !got.ChartRangeLines || !got.ChartMinMax || !got.ChartHideStats {
		t.Fatalf("LoadConfig after save = %+v, %v, want all four true", got, err)
	}
	set, err := s.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	style := set.ChartStyle
	if !style.AvgLine || !style.RangeLines || !style.MinMax || !style.HideStats {
		t.Errorf("Settings().ChartStyle = %+v, want all four true (this is what the real Strava chart photo uses)", style)
	}
}

// TestOverviewTogglesDefaultOnAndRoundTrip guards the same bug class as
// TestChartOverlayTogglesRoundTrip for the stats overview page's three
// show/hide toggles: migration 018 must add real columns, and LoadConfig/
// SaveConfig must actually read and write them, not just carry the Go
// struct fields.
func TestOverviewTogglesDefaultOnAndRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	c, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !c.OverviewShowTrend || !c.OverviewShowBySport || !c.OverviewShowTable {
		t.Fatalf("defaults = %+v, want all three true (new content should show up, not need finding)", c)
	}
	c.OverviewShowTrend, c.OverviewShowBySport, c.OverviewShowTable = false, false, false
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil || got.OverviewShowTrend || got.OverviewShowBySport || got.OverviewShowTable {
		t.Fatalf("LoadConfig after save = %+v, %v, want all three false", got, err)
	}
}

// TestOverviewGeneralTogglesDefaultOnAndRoundTrip guards the same bug class
// for migration 019's two toggles (whole-range glucose summary, per-source
// health list).
func TestOverviewGeneralTogglesDefaultOnAndRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	c, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !c.OverviewShowGeneral || !c.OverviewShowSourceHealth {
		t.Fatalf("defaults = %+v, want both true (new content should show up, not need finding)", c)
	}
	c.OverviewShowGeneral, c.OverviewShowSourceHealth = false, false
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil || got.OverviewShowGeneral || got.OverviewShowSourceHealth {
		t.Fatalf("LoadConfig after save = %+v, %v, want both false", got, err)
	}
}

// TestChartElevationDefaultOnAndRoundTrip guards migration 020's toggle,
// same bug class as TestOverviewGeneralTogglesDefaultOnAndRoundTrip.
func TestChartElevationDefaultOnAndRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	c, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !c.ChartElevation {
		t.Fatalf("default = %+v, want true (new content should show up, not need finding)", c)
	}
	c.ChartElevation = false
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil || got.ChartElevation {
		t.Fatalf("LoadConfig after save = %+v, %v, want false", got, err)
	}
}

// TestActivityDistanceElevationRoundTrip guards migration 020's
// distance_m/elevation_gain_m/elevation fields, same bug class as the
// existing heart_rate round trip this codebase already relies on.
func TestActivityDistanceElevationRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	a := &jobs.Activity{
		StravaID: "900", Sport: "Run", Start: t0, Duration: 30 * time.Minute, Status: jobs.StatusDone,
		Distance: 10031.4, ElevationGain: 143,
		Elevation: []chartimg.ElevPoint{{Time: t0, Meters: 100}, {Time: t0.Add(time.Minute), Meters: 110}},
	}
	if err := s.SaveActivity(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.Activity(ctx, "900")
	if err != nil || got == nil {
		t.Fatalf("Activity = %+v, %v", got, err)
	}
	if got.Distance != 10031.4 || got.ElevationGain != 143 {
		t.Errorf("Distance/ElevationGain = %v/%v, want 10031.4/143", got.Distance, got.ElevationGain)
	}
	if len(got.Elevation) != 2 || got.Elevation[1].Meters != 110 {
		t.Errorf("Elevation = %+v", got.Elevation)
	}
}

func TestListActivitiesAndEventsOrderAndChangedHook(t *testing.T) {
	changes := 0
	s := &PB{App: newApp(t), Changed: func() { changes++ }}
	ctx := context.Background()
	for i, id := range []string{"1", "2", "3"} {
		a := &jobs.Activity{StravaID: id, Start: t0.Add(time.Duration(i) * time.Hour), Duration: time.Hour, Status: jobs.StatusDone}
		if err := s.SaveActivity(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListActivities(ctx, 2)
	if err != nil || len(list) != 2 || list[0].StravaID != "3" || list[1].StravaID != "2" {
		t.Fatalf("list = %+v, %v", list, err)
	}

	_ = s.RecordEvent(ctx, jobs.Event{Type: jobs.EventStravaFailed, Severity: "error", Message: "first"})
	time.Sleep(5 * time.Millisecond) // created has millisecond resolution
	_ = s.RecordEvent(ctx, jobs.Event{Type: jobs.EventSessionExpired, Severity: "error", Message: "second"})
	evs, err := s.ListEvents(ctx, 10)
	if err != nil || len(evs) != 2 || evs[0].Message != "second" || evs[0].Notified {
		t.Fatalf("events = %+v, %v", evs, err)
	}
	if changes != 5 {
		t.Errorf("Changed called %d times, want 5 (3 activities + 2 events)", changes)
	}
}

func TestOriginalDescriptionRoundTripAndNeverCleared(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	a := &jobs.Activity{StravaID: "9", Start: t0, Duration: time.Hour, Status: jobs.StatusPending}
	if err := s.SaveActivity(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Activity(ctx, "9"); got.Original != nil {
		t.Fatalf("original = %v before backup", got.Original)
	}
	empty := ""
	a.Original = &empty
	if err := s.SaveActivity(ctx, a); err != nil {
		t.Fatal(err)
	}
	// A later save from a copy that never saw the backup must not erase it.
	if err := s.SaveActivity(ctx, &jobs.Activity{StravaID: "9", Start: t0, Status: jobs.StatusDone}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Activity(ctx, "9")
	if got.Original == nil || *got.Original != "" {
		t.Errorf("original = %v, want pointer to empty string", got.Original)
	}
}

func seedData(t *testing.T, s *PB) {
	t.Helper()
	ctx := context.Background()
	old, recent := time.Now().AddDate(0, 0, -400), time.Now().AddDate(0, 0, -1)
	if err := s.SaveSamples(ctx, "dexcom", []stats.Sample{{Time: old, Value: 100}, {Time: recent, Value: 120}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveActivity(ctx, &jobs.Activity{StravaID: "1", Name: "=HYPERLINK(\"x\")", Start: recent, Duration: time.Hour,
		Status: jobs.StatusDone, Summary: &stats.Summary{TIR: 90, Min: 70, Max: 150, Avg: 100}}); err != nil {
		t.Fatal(err)
	}
	_ = s.RecordEvent(ctx, jobs.Event{Type: jobs.EventStravaFailed, Severity: "error", Message: "x"})
}

func TestPruneKeepsRecentAndActivities(t *testing.T) {
	s := &PB{App: newApp(t)}
	seedData(t, s)
	cfg, _ := s.LoadConfig()
	if cfg.RetentionDays != 365 {
		t.Fatalf("default retention = %d", cfg.RetentionDays)
	}
	n, err := s.Prune(context.Background(), time.Now())
	if err != nil || n.Samples != 1 || n.Events != 0 {
		t.Fatalf("prune = %+v, %v", n, err)
	}
	if got, _ := s.LoadSamples(context.Background(), "dexcom", time.Now().AddDate(-2, 0, 0), time.Now()); len(got) != 1 {
		t.Errorf("samples left = %d", len(got))
	}
	acts, _ := s.ListActivities(context.Background(), 10)
	if len(acts) != 1 {
		t.Errorf("activities = %d, want untouched", len(acts))
	}

	cfg.RetentionDays = 0
	_ = s.SaveConfig(cfg)
	if n, _ := s.Prune(context.Background(), time.Now().AddDate(10, 0, 0)); n != (Counts{}) {
		t.Errorf("retention 0 deleted %+v", n)
	}
}

func TestPurgeAllKeepsSettingsAndSecrets(t *testing.T) {
	s := &PB{App: newApp(t)}
	seedData(t, s)
	n, err := s.PurgeAll(context.Background())
	if err != nil || n.Samples != 2 || n.Activities != 1 || n.Events != 1 {
		t.Fatalf("purge = %+v, %v", n, err)
	}
	if _, err := s.LoadConfig(); err != nil {
		t.Errorf("settings gone: %v", err)
	}
}

func TestExportCSV(t *testing.T) {
	s := &PB{App: newApp(t)}
	seedData(t, s)
	var b strings.Builder
	if err := s.ExportActivities(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.HasPrefix(out, "strava_id,name,") || !strings.Contains(out, `'=HYPERLINK`) || !strings.Contains(out, ",90.0,70,150,100.0,") {
		t.Errorf("activities csv:\n%s", out)
	}
	b.Reset()
	if err := s.ExportSamples(&b); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 3 || lines[0] != "time_utc,mg_dl,source" || !strings.HasSuffix(lines[1], ",100,dexcom") || !strings.Contains(lines[1], "T") {
		t.Errorf("samples csv:\n%s", b.String())
	}
}

func TestLatestSampleTimeAndMailBookkeeping(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	if _, ok := s.LatestSampleTime(ctx); ok {
		t.Error("an empty database has no latest reading")
	}
	_ = s.SaveSamples(ctx, "dexcom", []stats.Sample{{Time: t0, Value: 100}, {Time: t0.Add(10 * time.Minute), Value: 90}})
	_ = s.SaveSamples(ctx, "glooko", []stats.Sample{{Time: t0.Add(5 * time.Minute), Value: 95}})
	if got, ok := s.LatestSampleTime(ctx); !ok || !got.Equal(t0.Add(10*time.Minute)) {
		t.Errorf("latest = %v, %v", got, ok)
	}

	if !s.WeeklyLast().IsZero() || !s.HealthLast().IsZero() {
		t.Error("nothing sent yet")
	}
	when := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	if err := s.SetWeeklyLast(when); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHealthLast(when.AddDate(0, 0, 11)); err != nil {
		t.Fatal(err)
	}
	if !s.WeeklyLast().Equal(when) || !s.HealthLast().Equal(when.AddDate(0, 0, 11)) {
		t.Errorf("weekly=%v health=%v", s.WeeklyLast(), s.HealthLast())
	}
	cfg, _ := s.LoadConfig()
	if cfg.GapAlertHours != 3 || !cfg.MailAlerts || cfg.MailHealth {
		t.Errorf("defaults: %+v", cfg)
	}
}

func TestGapEventTypeIsAccepted(t *testing.T) {
	s := &PB{App: newApp(t)}
	if err := s.RecordEvent(context.Background(), jobs.Event{Type: jobs.EventGlucoseGap, Severity: "warning", Message: "gap"}); err != nil {
		t.Fatalf("migration 008 must allow glucose_gap: %v", err)
	}
}

func TestHeartRateStoredAndNeverCleared(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	a := jobs.Activity{StravaID: "9", Start: at, Duration: time.Hour, Status: jobs.StatusDone,
		HeartRate: []chartimg.HRPoint{{Time: at, BPM: 120}, {Time: at.Add(time.Minute), BPM: 130}}}
	if err := s.SaveActivity(ctx, &a); err != nil {
		t.Fatal(err)
	}
	// A later save without heart rate (a status update) must keep it.
	b := jobs.Activity{StravaID: "9", Start: at, Duration: time.Hour, Status: jobs.StatusProcessing}
	if err := s.SaveActivity(ctx, &b); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Activity(ctx, "9")
	if got == nil || len(got.HeartRate) != 2 || got.HeartRate[1].BPM != 130 || !got.HeartRate[0].Time.Equal(at) {
		t.Errorf("heart rate = %+v", got)
	}
}

func TestActivitiesCSVHasHeartRateColumns(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	_ = s.SaveActivity(ctx, &jobs.Activity{StravaID: "9", Start: at, Duration: time.Hour, Status: jobs.StatusDone,
		HeartRate: []chartimg.HRPoint{{Time: at.Add(time.Minute), BPM: 120}, {Time: at.Add(2 * time.Minute), BPM: 160}}})
	_ = s.SaveActivity(ctx, &jobs.Activity{StravaID: "10", Start: at, Duration: time.Hour, Status: jobs.StatusDone})
	var b strings.Builder
	if err := s.ExportActivities(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "processed_utc,hr_avg,hr_max,hr_min") || !strings.Contains(out, ",140,160,120") {
		t.Errorf("csv = %s", out)
	}
}

func TestRecoverStuckResetsProcessingAndReturnsThem(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	for _, seed := range []jobs.Activity{
		{StravaID: "p1", Start: t0, Duration: time.Hour, Status: jobs.StatusProcessing, Error: "attempt 1 failed"},
		{StravaID: "d1", Start: t0, Duration: time.Hour, Status: jobs.StatusDone},
		{StravaID: "f1", Start: t0, Duration: time.Hour, Status: jobs.StatusFailed},
	} {
		seed := seed
		if err := s.SaveActivity(ctx, &seed); err != nil {
			t.Fatal(err)
		}
	}
	stuck, err := s.RecoverStuck(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stuck) != 1 || stuck[0].StravaID != "p1" || stuck[0].Status != jobs.StatusPending {
		t.Fatalf("stuck = %+v", stuck)
	}
	got, _ := s.Activity(ctx, "p1")
	if got.Status != jobs.StatusPending || got.Error != "" {
		t.Errorf("not persisted: %+v", got)
	}
	for _, id := range []string{"d1", "f1"} {
		if got, _ := s.Activity(ctx, id); got.Status == jobs.StatusPending {
			t.Errorf("%s should not have been touched", id)
		}
	}
	// idempotent: nothing left stuck on a second call
	if stuck2, err := s.RecoverStuck(ctx); err != nil || len(stuck2) != 0 {
		t.Errorf("second call = %+v, %v", stuck2, err)
	}
}

func TestDueForBufferFiltersDoneAndUnbuffered(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	for _, seed := range []jobs.Activity{
		{StravaID: "ready", Start: t0, Duration: time.Hour, Status: jobs.StatusDone},
		{StravaID: "buffered", Start: t0, Duration: time.Hour, Status: jobs.StatusDone, BufferDone: true},
		{StravaID: "pending", Start: t0, Duration: time.Hour, Status: jobs.StatusPending},
		{StravaID: "old", Start: t0.Add(-48 * time.Hour), Duration: time.Hour, Status: jobs.StatusDone},
	} {
		seed := seed
		if err := s.SaveActivity(ctx, &seed); err != nil {
			t.Fatal(err)
		}
	}
	due, err := s.DueForBuffer(ctx, t0.Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].StravaID != "ready" {
		t.Fatalf("due = %+v", due)
	}
}

func TestBufferDoneRoundTripsAndNeverClears(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	a := &jobs.Activity{StravaID: "b1", Start: t0, Duration: time.Hour, Status: jobs.StatusDone, BufferDone: true}
	if err := s.SaveActivity(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Activity(ctx, "b1"); !got.BufferDone {
		t.Fatalf("BufferDone not persisted: %+v", got)
	}
	again := &jobs.Activity{StravaID: "b1", Start: t0, Duration: time.Hour, Status: jobs.StatusDone}
	if err := s.SaveActivity(ctx, again); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Activity(ctx, "b1"); !got.BufferDone {
		t.Errorf("BufferDone was cleared by a save that did not set it: %+v", got)
	}
}

func TestThresholdsMigrationDefaultsAndRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VeryLow != 54 || cfg.VeryHigh != 250 {
		t.Fatalf("defaults = %v/%v, want 54/250", cfg.VeryLow, cfg.VeryHigh)
	}
	cfg.VeryLow, cfg.VeryHigh = 60, 230
	if err := s.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadConfig()
	if got.VeryLow != 60 || got.VeryHigh != 230 {
		t.Errorf("round trip = %v/%v", got.VeryLow, got.VeryHigh)
	}
	rng, err := s.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if vl, vh := rng.Range.Thresholds(); vl != 60 || vh != 230 {
		t.Errorf("Settings range thresholds = %v/%v", vl, vh)
	}
}

func TestLoadSamplesFastMatchesAny(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	var a, b []stats.Sample
	for i := 0; i < 40; i++ {
		a = append(a, stats.Sample{Time: t0.Add(time.Duration(i) * 5 * time.Minute), Value: 100 + float64(i)})
		if i%3 == 0 { // duplicate timestamps from a second source stay duplicated
			b = append(b, stats.Sample{Time: t0.Add(time.Duration(i) * 5 * time.Minute), Value: 90 + float64(i)})
		}
	}
	if err := s.SaveSamples(ctx, "dexcom", a); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSamples(ctx, "glooko", b); err != nil {
		t.Fatal(err)
	}
	from, to := t0.Add(10*time.Minute), t0.Add(150*time.Minute)
	want, err := s.LoadSamplesAny(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadSamplesFast(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || len(got) == 0 {
		t.Fatalf("len fast=%d any=%d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Time.Equal(want[i].Time) {
			t.Fatalf("[%d] time %v != %v", i, got[i].Time, want[i].Time)
		}
	}
	// Equal-timestamp order between sources is unspecified; compare as multisets.
	sum := func(xs []stats.Sample) (f float64) {
		for _, x := range xs {
			f += x.Value
		}
		return
	}
	if sum(got) != sum(want) {
		t.Errorf("value sums differ: %v vs %v", sum(got), sum(want))
	}
	if empty, err := s.LoadSamplesFast(ctx, t0.Add(-48*time.Hour), t0.Add(-47*time.Hour)); err != nil || len(empty) != 0 {
		t.Errorf("empty range = %v, %v", empty, err)
	}
}

func TestActivitiesInRangeLightMatchesFull(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	for i, name := range []string{"Run", "Ride"} {
		a := &jobs.Activity{StravaID: string(rune('1' + i)), Name: name, Sport: name, Start: t0.Add(time.Duration(i) * time.Hour),
			Duration: 30 * time.Minute, Distance: 5000, ElevationGain: 42, Status: jobs.StatusDone, Attempts: 2,
			ChartUploaded: i == 0, BufferDone: true, Summary: &stats.Summary{Count: 6, TIR: 80, Avg: 115},
			HeartRate: []chartimg.HRPoint{{Time: t0, BPM: 140}}}
		if err := s.SaveActivity(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	from, to := t0.Add(-time.Hour), t0.Add(3*time.Hour)
	full, err := s.ActivitiesInRange(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	light, err := s.ActivitiesInRangeLight(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(light) != 2 || len(full) != 2 {
		t.Fatalf("len light=%d full=%d", len(light), len(full))
	}
	for i := range full {
		f, l := full[i], light[i]
		if f.StravaID != l.StravaID || f.Name != l.Name || f.Sport != l.Sport || !f.Start.Equal(l.Start) ||
			f.Duration != l.Duration || f.Distance != l.Distance || f.ElevationGain != l.ElevationGain ||
			f.Status != l.Status || f.Attempts != l.Attempts || f.ChartUploaded != l.ChartUploaded || f.BufferDone != l.BufferDone {
			t.Errorf("[%d] light %+v != full %+v", i, l, f)
		}
		if l.Summary == nil || l.Summary.TIR != 80 {
			t.Errorf("[%d] summary lost: %+v", i, l.Summary)
		}
		if l.HeartRate != nil || l.Elevation != nil {
			t.Errorf("[%d] streams should be skipped", i)
		}
	}
}

func TestDataVersionChanges(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	v0, err := s.DataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v1, _ := s.DataVersion(ctx); v1 != v0 {
		t.Error("version changed with no writes")
	}
	_ = s.SaveSamples(ctx, "dexcom", []stats.Sample{{Time: t0, Value: 100}})
	v1, _ := s.DataVersion(ctx)
	if v1 == v0 || v1.Samples != 1 || !v1.LatestSample.Equal(t0) {
		t.Errorf("after sample: %+v", v1)
	}
	_ = s.SaveActivity(ctx, &jobs.Activity{StravaID: "9", Name: "R", Start: t0, Duration: time.Minute, Status: jobs.StatusDone})
	v2, _ := s.DataVersion(ctx)
	if v2 == v1 || v2.Activities != 1 {
		t.Errorf("after activity: %+v", v2)
	}
}

func BenchmarkLoadSamples100k(b *testing.B) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: b.TempDir()})
	if err := app.Bootstrap(); err != nil {
		b.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = app.ClearBootstrap() }()
	s := &PB{App: app}
	ctx := context.Background()
	const n = 100_000
	samples := make([]stats.Sample, n)
	for i := range samples {
		samples[i] = stats.Sample{Time: t0.Add(time.Duration(i) * 5 * time.Minute), Value: 100 + float64(i%80)}
	}
	for i := 0; i < n; i += 5000 {
		if err := s.SaveSamples(ctx, "dexcom", samples[i:i+5000]); err != nil {
			b.Fatal(err)
		}
	}
	from, to := t0, t0.Add(n*5*time.Minute)
	b.Run("fast", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if got, err := s.LoadSamplesFast(ctx, from, to); err != nil || len(got) != n {
				b.Fatal(len(got), err)
			}
		}
	})
	b.Run("any", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if got, err := s.LoadSamplesAny(ctx, from, to); err != nil || len(got) != n {
				b.Fatal(len(got), err)
			}
		}
	})
}

func TestParseStoredMatchesTimeParse(t *testing.T) {
	for _, in := range []string{"2026-09-20 07:00:00.000Z", "2026-12-31 23:59:59.999Z", "2024-02-29 00:00:00.500Z"} {
		want, err := time.Parse(pbStoredTime, in)
		got, ok := parseStored(in)
		if err != nil || !ok || !got.Equal(want) {
			t.Errorf("%s: got %v %v, want %v", in, got, ok, want)
		}
	}
	if _, ok := parseStored("garbage"); ok {
		t.Error("garbage parsed")
	}
}
