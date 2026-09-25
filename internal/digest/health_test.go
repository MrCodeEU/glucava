package digest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
)

var (
	augFrom = time.Date(2026, 8, 1, 0, 0, 0, 0, vienna)
	augTo   = time.Date(2026, 9, 1, 0, 0, 0, 0, vienna)
)

// hourly returns one reading per hour for the given number of hours from augFrom.
func hourly(hours int) []stats.Sample {
	var s []stats.Sample
	for i := 0; i < hours; i++ {
		s = append(s, stats.Sample{Time: augFrom.Add(time.Duration(i) * time.Hour), Value: 100})
	}
	return s
}

func TestCoverageCountsHoursNotReadings(t *testing.T) {
	if got := coverage(hourly(31*24), augFrom, augTo); got != 100 {
		t.Errorf("full month = %v", got)
	}
	if got := coverage(hourly(31*12), augFrom, augTo); got != 50 {
		t.Errorf("half month = %v", got)
	}
	// Ten readings within one hour still count as one hour.
	var dense []stats.Sample
	for i := 0; i < 10; i++ {
		dense = append(dense, stats.Sample{Time: augFrom.Add(time.Duration(i) * 5 * time.Minute)})
	}
	if got := coverage(dense, augFrom, augTo); got*float64(31*24)/100 != 1 {
		t.Errorf("dense readings should cover one hour, got %v%%", got)
	}
	if coverage(nil, augFrom, augFrom) != 0 {
		t.Error("empty period")
	}
}

func TestHealthMessageHealthy(t *testing.T) {
	in := HealthInput{
		Activities: []jobs.Activity{act("1", augFrom.AddDate(0, 0, 3), 90, 80, 0), act("2", augFrom.AddDate(0, 0, 10), 80, 70, 0)},
		Samples:    hourly(31 * 24), From: augFrom, To: augTo, Build: "v1.2.3",
	}
	m := HealthMessage(in, vienna)
	if m.Severity != "info" || m.Type != notify.TypeHealthReport || !strings.Contains(m.Title, "August 2026") || !strings.Contains(m.Body, "healthy") {
		t.Fatalf("%+v", m)
	}
	if factOf(m, "Activities annotated") != "2" || factOf(m, "Glucose data coverage") != "100%" || factOf(m, "glucava version") != "v1.2.3" {
		t.Errorf("facts = %+v", m.Facts)
	}
}

func TestHealthMessageFlagsProblems(t *testing.T) {
	bad := act("3", augFrom.AddDate(0, 0, 5), 0, 0, 0)
	bad.Status = jobs.StatusFailed
	in := HealthInput{
		Activities: []jobs.Activity{bad, act("outside", augTo.AddDate(0, 0, 2), 90, 80, 0)},
		Events: []store.EventRow{
			{Type: "session_expired", Created: augFrom.AddDate(0, 0, 4)},
			{Type: "session_expired", Created: augFrom.AddDate(0, 0, 6)},
			{Type: "canary_failed", Created: augTo.AddDate(0, 0, 1)}, // next month: not counted
		},
		Samples: hourly(31 * 12), From: augFrom, To: augTo,
	}
	m := HealthMessage(in, vienna)
	if m.Severity != "warning" || !strings.Contains(m.Body, "1 activities failed") || !strings.Contains(m.Body, "50%") {
		t.Errorf("body = %q sev=%s", m.Body, m.Severity)
	}
	if factOf(m, "Activities annotated") != "0" || factOf(m, "Alert: Strava session expired") != "2" || factOf(m, "Alert: Strava canary check failed") != "" {
		t.Errorf("facts = %+v", m.Facts)
	}
}

type fakeHealthStore struct {
	fakeStore
	events []store.EventRow
	smp    []stats.Sample
	hlast  time.Time
}

func (f *fakeHealthStore) ListEvents(context.Context, int) ([]store.EventRow, error) {
	return f.events, nil
}
func (f *fakeHealthStore) LoadSamplesAny(context.Context, time.Time, time.Time) ([]stats.Sample, error) {
	return f.smp, nil
}
func (f *fakeHealthStore) HealthLast() time.Time           { return f.hlast }
func (f *fakeHealthStore) SetHealthLast(t time.Time) error { f.hlast = t; return nil }

func TestHealthSendsOncePerMonth(t *testing.T) {
	st := &fakeHealthStore{smp: hourly(31 * 24)}
	now := time.Date(2026, 9, 1, 7, 0, 0, 0, vienna)
	on := true
	var sent []notify.Message
	h := &Health{
		Store: st, Enabled: func() bool { return on }, Loc: func() *time.Location { return vienna }, Now: func() time.Time { return now },
		Send: func(_ context.Context, m notify.Message) error { sent = append(sent, m); return nil },
	}
	if ok, _ := h.Once(context.Background()); ok {
		t.Fatal("sent before 08:00 on the 1st")
	}
	now = now.Add(2 * time.Hour)
	on = false
	if ok, _ := h.Once(context.Background()); ok {
		t.Fatal("sent while disabled")
	}
	on = true
	if ok, err := h.Once(context.Background()); !ok || err != nil || len(sent) != 1 {
		t.Fatalf("ok=%v err=%v sent=%d", ok, err, len(sent))
	}
	if !strings.Contains(sent[0].Title, "August 2026") {
		t.Errorf("title = %q", sent[0].Title)
	}
	now = now.AddDate(0, 0, 10)
	if ok, _ := h.Once(context.Background()); ok || len(sent) != 1 {
		t.Error("sent twice in one month")
	}
	now = time.Date(2026, 10, 1, 9, 0, 0, 0, vienna)
	if ok, _ := h.Once(context.Background()); !ok || !strings.Contains(sent[1].Title, "September 2026") {
		t.Error("next month not sent")
	}
}
