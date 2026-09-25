// Package demo provides made-up data and stand-ins for Strava and Dexcom, so the
// web UI can be tried and screenshotted without any account.
package demo

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
)

// SourceName is the key demo readings are stored under.
const SourceName = "demo"

// Curve returns a plausible glucose value in mg/dL at t for an activity that
// starts at start. It is deterministic, so a reprocessed activity looks the same.
func Curve(t, start time.Time, seed uint32) float64 {
	h := float64(t.Sub(start)) / float64(time.Hour) // hours since the start
	base := 112 + 14*math.Sin(float64(seed%97)/97*2*math.Pi+h*0.9)
	meal := 0.0
	if h < -0.3 { // a meal before the run gives a small hump
		meal = 38 * math.Exp(-sq((h+1.1)/0.5))
	}
	exercise := 0.0
	if h >= 0 && h <= 1.1 { // glucose falls during the run, then rebounds
		exercise = -30 * math.Sin(math.Pi*h/1.1)
	}
	if h > 1.1 {
		exercise = 12 * math.Exp(-(h-1.1)/0.6)
	}
	noise := 5 * math.Sin(h*11+float64(seed%13))
	// Every few activities dip low or run high, to show out-of-range colours.
	extra := 0.0
	switch seed % 5 {
	case 1:
		extra = -22 * math.Exp(-sq((h-0.8)/0.3))
	case 3:
		extra = 55 * math.Exp(-sq((h+0.2)/0.35))
	}
	return math.Max(48, base+meal+exercise+noise+extra)
}

func seedOf(id string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return h.Sum32()
}

// samples builds 5-minute readings for [from, to] around an activity start.
func samples(id string, start, from, to time.Time) []stats.Sample {
	seed := seedOf(id)
	var out []stats.Sample
	for t := from.Truncate(5 * time.Minute); !t.After(to); t = t.Add(5 * time.Minute) {
		if t.Before(from) {
			continue
		}
		out = append(out, stats.Sample{Time: t, Value: math.Round(Curve(t, start, seed))})
	}
	return out
}

// PreWindow is the glucose window before an activity start that demo mode uses.
const PreWindow = 30 * time.Minute

// Source is a glucose.Source that invents readings for any window. It assumes the
// window begins PreWindow before the activity, which Seed sets in the settings.
type Source struct{}

// Samples implements glucose.Source.
func (Source) Samples(_ context.Context, from, to time.Time) ([]stats.Sample, error) {
	start := from.Add(PreWindow)
	return samples(start.Format(time.RFC3339), start, from, to), nil
}

// Writer is a jobs.Writer that keeps descriptions in memory and takes a moment,
// so live progress is visible.
type Writer struct {
	Delay time.Duration

	mu   sync.Mutex
	desc map[string]string
}

// UpdateDescription implements jobs.Writer.
func (w *Writer) UpdateDescription(ctx context.Context, id string, merge func(string) string) error {
	select {
	case <-time.After(w.Delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.desc == nil {
		w.desc = map[string]string{}
	}
	w.desc[id] = merge(w.desc[id])
	return nil
}

// UploadPhoto implements jobs.PhotoWriter. Nothing is stored; the demo only
// shows the step happening.
func (w *Writer) UploadPhoto(ctx context.Context, _, _ string, _ []byte) error {
	select {
	case <-time.After(w.Delay / 2):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Session is a SessionChecker that always succeeds after a short pause.
type Session struct{}

// CheckSession implements the web.SessionChecker interface.
func (Session) CheckSession(ctx context.Context) error {
	select {
	case <-time.After(1200 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Lister is a poll.Lister. It reports the seeded activities, and each poll after
// the first invents one new activity so the live flow can be watched.
type Lister struct {
	Store *store.PB
	Now   func() time.Time

	mu    sync.Mutex
	calls int
	added []jobs.Activity
}

var newNames = []string{"Lunch Run", "Tempo Run", "Hill Repeats", "Evening Jog", "Progression Run"}

// ListRecent implements poll.Lister.
func (l *Lister) ListRecent(ctx context.Context, limit int) ([]jobs.Activity, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	now := l.now()

	if l.calls > 1 && len(l.added) < len(newNames) {
		n := len(l.added)
		d := time.Duration(35+n*7) * time.Minute
		l.added = append(l.added, jobs.Activity{
			StravaID: fmt.Sprintf("9000%d", n+1), Name: newNames[n], Sport: "Run",
			Start: now.Add(-d - 50*time.Minute), Duration: d, Status: jobs.StatusPending,
		})
	}
	seeded, err := l.Store.ListActivities(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := append([]jobs.Activity(nil), l.added...)
	return append(reverse(out), seeded...), nil // new activities first
}

func reverse(a []jobs.Activity) []jobs.Activity {
	for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
		a[i], a[j] = a[j], a[i]
	}
	return a
}

func (l *Lister) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// Seed fills an empty database with two weeks of activities, glucose readings and
// notifications. It does nothing if activities already exist.
func Seed(ctx context.Context, st *store.PB, now time.Time) error {
	if n, err := st.App.CountRecords("activities"); err != nil || n > 0 {
		return err
	}
	cfg, err := st.LoadConfig()
	if err != nil {
		return err
	}
	cfg.DexcomUsername = "demo@example.test"
	cfg.PreMin = int(PreWindow / time.Minute)
	if err := st.SaveConfig(cfg); err != nil {
		return err
	}
	rng := stats.Range{Low: cfg.RangeLow, High: cfg.RangeHigh}
	post := time.Duration(cfg.PostMin) * time.Minute

	type spec struct {
		name  string
		mins  int
		daysA float64 // days ago
		hour  int
	}
	specs := []spec{
		{"Morning Run", 48, 0.4, 7}, {"Easy Run", 32, 1.5, 18}, {"Long Run", 95, 3.2, 8}, {"Intervals", 41, 4.1, 17},
		{"Recovery Jog", 27, 5.5, 7}, {"Tempo Run", 55, 6.6, 12}, {"Trail Run", 72, 8.3, 9}, {"Morning Run", 45, 9.4, 6},
		{"Easy Run", 36, 10.6, 18}, {"Hill Repeats", 50, 11.7, 17}, {"Long Run", 88, 12.8, 8}, {"Evening Jog", 30, 13.9, 19},
	}

	for i, sp := range specs {
		id := fmt.Sprintf("%d", 140100-i)
		day := now.Add(-time.Duration(sp.daysA * 24 * float64(time.Hour)))
		start := time.Date(day.Year(), day.Month(), day.Day(), sp.hour, (i*13)%50, 0, 0, day.Location())
		dur := time.Duration(sp.mins) * time.Minute
		a := &jobs.Activity{StravaID: id, Name: sp.name, Sport: "Run", Start: start, Duration: dur, Status: jobs.StatusDone, Attempts: 1}

		// The failed activity has no readings, like a real outage would leave.
		if i == 2 {
			a.Status, a.Attempts = jobs.StatusFailed, 4
			a.Error = "jobs: no glucose data for the activity window"
			if err := st.SaveActivity(ctx, a); err != nil {
				return err
			}
			continue
		}
		smp := samples(start.Format(time.RFC3339), start, start.Add(-PreWindow), start.Add(dur).Add(post))
		if err := st.SaveSamples(ctx, SourceName, smp); err != nil {
			return err
		}
		if sum, ok := stats.Summarize(smp, rng); ok {
			a.Summary = &sum
		}
		if err := st.SaveActivity(ctx, a); err != nil {
			return err
		}
	}

	events := []struct {
		e        jobs.Event
		notified bool
	}{
		{jobs.Event{Type: jobs.EventGlucoseUnavailable, Severity: "error", StravaID: "140098",
			Message: "activity 140098: jobs: no glucose data for the activity window"}, true},
		{jobs.Event{Type: jobs.EventSessionExpired, Severity: "error",
			Message: "polling failed: the Strava session has expired, import fresh cookies"}, true},
		{jobs.Event{Type: "trigger_rejected", Severity: "warning",
			Message: "trigger call from 203.0.113.7 rejected: invalid token"}, false},
	}
	for _, ev := range events {
		if err := st.RecordEvent(ctx, ev.e); err != nil {
			return err
		}
	}
	// Mark the older ones as delivered.
	pending, err := st.Pending(ctx)
	if err != nil {
		return err
	}
	for i, p := range pending {
		if events[i].notified {
			if err := st.MarkNotified(ctx, p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func sq(x float64) float64 { return x * x }
