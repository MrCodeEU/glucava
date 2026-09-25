package gap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
)

func setup(last time.Time, has bool, limit time.Duration) (*Monitor, *[]jobs.Event, *time.Time) {
	now := last.Add(time.Hour)
	var events []jobs.Event
	m := &Monitor{
		Latest:    func(context.Context) (time.Time, bool) { return last, has },
		Threshold: func() time.Duration { return limit },
		Record:    func(_ context.Context, e jobs.Event) error { events = append(events, e); return nil },
		Loc:       func() *time.Location { return time.UTC },
		Now:       func() time.Time { return now },
	}
	return m, &events, &now
}

var t0 = time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

func TestNoAlertWithinThreshold(t *testing.T) {
	m, ev, _ := setup(t0, true, 3*time.Hour)
	if ok, err := m.Once(context.Background()); ok || err != nil || len(*ev) != 0 {
		t.Errorf("ok=%v err=%v events=%v", ok, err, *ev)
	}
}

func TestOneAlertPerGapThenRearms(t *testing.T) {
	m, ev, now := setup(t0, true, 3*time.Hour)
	*now = t0.Add(4*time.Hour + 10*time.Minute)
	if ok, _ := m.Once(context.Background()); !ok || len(*ev) != 1 {
		t.Fatalf("no alert: %v", *ev)
	}
	e := (*ev)[0]
	if e.Type != jobs.EventGlucoseGap || e.Severity != "warning" || !strings.Contains(e.Message, "4h 10m") || !strings.Contains(e.Message, "10:00") {
		t.Errorf("event = %+v", e)
	}
	*now = t0.Add(9 * time.Hour)
	if ok, _ := m.Once(context.Background()); ok || len(*ev) != 1 {
		t.Error("the same gap alerted twice")
	}
	// Readings return, then a new gap: alert again.
	back := *now
	m.Latest = func(context.Context) (time.Time, bool) { return back, true }
	_, _ = m.Once(context.Background())
	*now = now.Add(5 * time.Hour)
	if ok, _ := m.Once(context.Background()); !ok || len(*ev) != 2 {
		t.Errorf("new gap not reported: %d events", len(*ev))
	}
}

func TestOffAndNeverHadData(t *testing.T) {
	m, ev, now := setup(t0, true, 0)
	*now = t0.Add(48 * time.Hour)
	if ok, _ := m.Once(context.Background()); ok || len(*ev) != 0 {
		t.Error("alerted while turned off")
	}
	m, ev, now = setup(time.Time{}, false, time.Hour)
	*now = t0.Add(48 * time.Hour)
	if ok, _ := m.Once(context.Background()); ok || len(*ev) != 0 {
		t.Error("a fresh install with no readings is not a gap")
	}
}

func TestRecordFailureRetries(t *testing.T) {
	m, _, now := setup(t0, true, time.Hour)
	*now = t0.Add(5 * time.Hour)
	fail := true
	var got int
	m.Record = func(context.Context, jobs.Event) error {
		if fail {
			return errors.New("db down")
		}
		got++
		return nil
	}
	if _, err := m.Once(context.Background()); err == nil {
		t.Fatal("expected the record error")
	}
	fail = false
	if ok, _ := m.Once(context.Background()); !ok || got != 1 {
		t.Errorf("did not retry after a failed record: ok=%v got=%d", ok, got)
	}
}
