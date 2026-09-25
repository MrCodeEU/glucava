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
	if set.Unit != render.MgDL || set.Range != stats.DefaultRange || set.Post != 30*time.Minute || set.Pre != 0 || set.PollInterval != 10*time.Minute {
		t.Errorf("settings = %+v", set)
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
