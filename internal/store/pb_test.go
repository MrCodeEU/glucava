package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	_ "github.com/pocketbase/pocketbase/migrations" // registers the system migrations

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

func TestNotifySettings(t *testing.T) {
	app := newApp(t)
	s := &PB{App: app}
	if n, w, err := s.NotifySettings(); err != nil || n != "" || w != "" {
		t.Fatalf("defaults = %q %q %v", n, w, err)
	}
	recs, _ := app.FindRecordsByFilter("settings", "", "", 1, 0)
	recs[0].Set("ntfy_url", "https://ntfy.example/t")
	if err := app.Save(recs[0]); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := s.NotifySettings(); n != "https://ntfy.example/t" {
		t.Errorf("ntfy = %q", n)
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
