package digest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/chartimg"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

var vienna, _ = time.LoadLocation("Europe/Vienna")

func act(id string, start time.Time, tir, min float64, below float64) jobs.Activity {
	return jobs.Activity{
		StravaID: id, Name: "Run " + id, Sport: "Run", Start: start, Duration: 45 * time.Minute, Status: jobs.StatusDone,
		Summary: &stats.Summary{Count: 10, TIR: tir, Min: min, Max: 180, Avg: 120, Below: below, Start: 100, End: 110},
	}
}

func factOf(m notify.Message, label string) string {
	for _, f := range m.Facts {
		if f.Label == label {
			return f.Value
		}
	}
	return ""
}

func TestActivityMessage(t *testing.T) {
	a := act("1", time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC), 92, 78, 0)
	m, ok := ActivityMessage(a, render.MgDL, stats.DefaultRange, nil, vienna)
	if !ok || m.Type != notify.TypeActivitySummary || m.StravaID != "1" || m.Severity != "info" {
		t.Fatalf("%+v ok=%v", m, ok)
	}
	if factOf(m, "Time in range") != "92%" || factOf(m, "Lowest") != "78 mg/dL" || factOf(m, "Duration") != "45m" {
		t.Errorf("facts = %+v", m.Facts)
	}
	if !strings.Contains(m.Body, "Mon 21 Sep, 08:00") {
		t.Errorf("body should use the local time: %q", m.Body)
	}
	a.Summary.Below = 4
	if m, _ := ActivityMessage(a, render.MgDL, stats.DefaultRange, nil, vienna); m.Severity != "warning" {
		t.Error("a low should raise the severity")
	}
	if m, _ := ActivityMessage(a, render.MmolL, stats.DefaultRange, nil, vienna); factOf(m, "Lowest") != "4.3 mmol/L" {
		t.Errorf("mmol/L: %q", factOf(m, "Lowest"))
	}
	a.Summary = nil
	if _, ok := ActivityMessage(a, render.MgDL, stats.DefaultRange, nil, vienna); ok {
		t.Error("no summary, no mail")
	}
}

func TestWeekStart(t *testing.T) {
	// Wed 23 Sep 2026 12:00 local -> Mon 21 Sep 08:00 local.
	got := WeekStart(time.Date(2026, 9, 23, 12, 0, 0, 0, vienna), vienna)
	if want := time.Date(2026, 9, 21, 8, 0, 0, 0, vienna); !got.Equal(want) {
		t.Errorf("got %v want %v", got, want)
	}
	// Sunday belongs to the week that started six days earlier.
	got = WeekStart(time.Date(2026, 9, 27, 23, 0, 0, 0, vienna), vienna)
	if want := time.Date(2026, 9, 21, 8, 0, 0, 0, vienna); !got.Equal(want) {
		t.Errorf("sunday: got %v", got)
	}
}

type fakeStore struct {
	acts []jobs.Activity
	last time.Time
}

func (f *fakeStore) ListActivities(context.Context, int) ([]jobs.Activity, error) { return f.acts, nil }
func (f *fakeStore) WeeklyLast() time.Time                                        { return f.last }
func (f *fakeStore) SetWeeklyLast(t time.Time) error                              { f.last = t; return nil }

func TestWeeklySendsOncePerWeek(t *testing.T) {
	mon := time.Date(2026, 9, 21, 0, 0, 0, 0, vienna)
	st := &fakeStore{acts: []jobs.Activity{
		act("a", mon.AddDate(0, 0, -6).Add(7*time.Hour), 90, 80, 0),  // last week
		act("b", mon.AddDate(0, 0, -3).Add(18*time.Hour), 70, 60, 8), // last week, with a low
		act("c", mon.AddDate(0, 0, -10), 95, 90, 0),                  // the week before
		act("d", mon.AddDate(0, 0, 1), 10, 40, 50),                   // this week: not included
	}}
	var sent []notify.Message
	now := mon.Add(9 * time.Hour)
	on := true
	w := &Weekly{
		Store: st, Enabled: func() bool { return on }, Unit: func() render.Unit { return render.MgDL },
		Loc: func() *time.Location { return vienna }, Now: func() time.Time { return now },
		Send: func(_ context.Context, m notify.Message) error { sent = append(sent, m); return nil },
	}

	// Before Monday 08:00: nothing.
	now = mon.Add(7 * time.Hour)
	if ok, _ := w.Once(context.Background()); ok || len(sent) != 0 {
		t.Fatal("sent before Monday 08:00")
	}
	now = mon.Add(9 * time.Hour)
	on = false
	if ok, _ := w.Once(context.Background()); ok {
		t.Fatal("sent while disabled")
	}
	on = true
	if ok, err := w.Once(context.Background()); !ok || err != nil || len(sent) != 1 {
		t.Fatalf("ok=%v err=%v sent=%d", ok, err, len(sent))
	}
	m := sent[0]
	if factOf(m, "Activities") != "2" || factOf(m, "Average time in range") != "80%" ||
		factOf(m, "Change vs the week before") != "-15 points" || factOf(m, "Activities with a low") != "1 of 2" || m.Severity != "warning" {
		t.Errorf("facts = %+v", m.Facts)
	}
	if !strings.Contains(m.Title, "14 Sep – 20 Sep") {
		t.Errorf("title = %q", m.Title)
	}
	// Not again the same week, even hours later.
	now = mon.Add(30 * time.Hour)
	if ok, _ := w.Once(context.Background()); ok || len(sent) != 1 {
		t.Error("sent twice in one week")
	}
	// The next Monday it is due again.
	now = mon.AddDate(0, 0, 7).Add(9 * time.Hour)
	if _, err := w.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWeeklyQuietWeekSendsANote(t *testing.T) {
	st := &fakeStore{}
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, vienna)
	var sent []notify.Message
	w := &Weekly{
		Store: st, Enabled: func() bool { return true }, Unit: func() render.Unit { return render.MgDL },
		Loc: func() *time.Location { return vienna }, Now: func() time.Time { return now },
		Send: func(_ context.Context, m notify.Message) error { sent = append(sent, m); return nil },
	}
	if ok, err := w.Once(context.Background()); !ok || err != nil || len(sent) != 1 || st.last.IsZero() {
		t.Fatalf("ok=%v err=%v sent=%d last=%v", ok, err, len(sent), st.last)
	}
	if m := sent[0]; len(m.Facts) != 0 || !strings.Contains(m.Body, "No activity") || m.Icon == "" || m.Type != notify.TypeWeeklySummary {
		t.Errorf("quiet note = %+v", m)
	}
	if ok, _ := w.Once(context.Background()); ok || len(sent) != 1 {
		t.Error("the quiet note was sent twice")
	}
}

func TestActivityMessageHasChartAndSportIcon(t *testing.T) {
	a := act("1", time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC), 92, 78, 0)
	a.Sport = "Ride"
	samples := []stats.Sample{{Time: a.Start, Value: 110}, {Time: a.Start.Add(20 * time.Minute), Value: 95}}
	m, _ := ActivityMessage(a, render.MgDL, stats.DefaultRange, samples, vienna)
	if len(m.Chart) == 0 || m.ChartAlt == "" || m.Icon != "\U0001F6B4" {
		t.Errorf("chart=%d bytes alt=%q icon=%q", len(m.Chart), m.ChartAlt, m.Icon)
	}
	if m, _ := ActivityMessage(a, render.MgDL, stats.DefaultRange, nil, vienna); len(m.Chart) != 0 {
		t.Error("no samples, no chart")
	}
	a.Sport = "Curling"
	if m, _ := ActivityMessage(a, render.MgDL, stats.DefaultRange, nil, vienna); m.Icon != "\U0001F3C5" {
		t.Errorf("unknown sport icon = %q", m.Icon)
	}
}

func TestWeeklyMessageHasBarChart(t *testing.T) {
	cur := []jobs.Activity{act("a", time.Date(2026, 9, 15, 6, 0, 0, 0, time.UTC), 90, 80, 0)}
	m, ok := WeeklyMessage(cur, nil, time.Date(2026, 9, 14, 0, 0, 0, 0, vienna), time.Date(2026, 9, 21, 0, 0, 0, 0, vienna), render.MgDL, vienna)
	if !ok || len(m.Chart) == 0 {
		t.Fatalf("ok=%v chart=%d", ok, len(m.Chart))
	}
}

func TestActivityMessageIncludesHeartRateWhenKnown(t *testing.T) {
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	a := jobs.Activity{StravaID: "1", Name: "Run", Start: at, Duration: time.Hour, Summary: &stats.Summary{TIR: 90, Count: 5},
		HeartRate: []chartimg.HRPoint{{Time: at.Add(time.Minute), BPM: 120}, {Time: at.Add(2 * time.Minute), BPM: 160}}}
	m, ok := ActivityMessage(a, render.MgDL, stats.DefaultRange, nil, time.UTC)
	if !ok {
		t.Fatal("no message")
	}
	found := false
	for _, f := range m.Facts {
		if f.Label == "Heart rate, average / max" && f.Value == "140 / 160 bpm" {
			found = true
		}
	}
	if !found {
		t.Errorf("facts = %+v", m.Facts)
	}
	a.HeartRate = nil
	m, _ = ActivityMessage(a, render.MgDL, stats.DefaultRange, nil, time.UTC)
	for _, f := range m.Facts {
		if strings.HasPrefix(f.Label, "Heart rate") {
			t.Error("heart rate fact without heart rate")
		}
	}
}
