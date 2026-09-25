package jobs

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/chartimg"
	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

var start = time.Date(2026, 9, 20, 7, 0, 0, 0, time.UTC)

type memStore struct {
	mu         sync.Mutex
	acts       map[string]Activity
	samples    []stats.Sample
	events     []Event
	set        Settings
	savedCalls int
	failSave   bool // fail SaveActivity when the activity carries a backup
}

func newStore() *memStore {
	return &memStore{acts: map[string]Activity{}, set: Settings{
		Unit: render.MgDL, Range: stats.DefaultRange, Post: 30 * time.Minute,
	}}
}

func (m *memStore) Settings(context.Context) (Settings, error) { return m.set, nil }
func (m *memStore) Activity(_ context.Context, id string) (*Activity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.acts[id]; ok {
		return &a, nil
	}
	return nil, nil
}
func (m *memStore) SaveActivity(_ context.Context, a *Activity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSave && a.Original != nil {
		return errors.New("disk full")
	}
	if a.Original == nil { // like the real store, never clear a stored backup
		a.Original = m.acts[a.StravaID].Original
	}
	a.ChartUploaded = a.ChartUploaded || m.acts[a.StravaID].ChartUploaded // never cleared, like the real store
	m.acts[a.StravaID] = *a
	m.savedCalls++
	return nil
}
func (m *memStore) SaveSamples(_ context.Context, _ string, s []stats.Sample) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples, s...)
	return nil
}
func (m *memStore) LoadSamples(_ context.Context, _ string, from, to time.Time) ([]stats.Sample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []stats.Sample
	for _, s := range m.samples {
		if !s.Time.Before(from) && !s.Time.After(to) {
			out = append(out, s)
		}
	}
	return out, nil
}
func (m *memStore) LoadSamplesAny(ctx context.Context, from, to time.Time) ([]stats.Sample, error) {
	return m.LoadSamples(ctx, "", from, to) // memStore never partitioned by source
}
func (m *memStore) RecordEvent(_ context.Context, e Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

type fakeSource struct {
	samples []stats.Sample
	errs    []error // returned in order, then samples
	calls   int
}

func (f *fakeSource) Samples(context.Context, time.Time, time.Time) ([]stats.Sample, error) {
	f.calls++
	if len(f.errs) > 0 {
		e := f.errs[0]
		f.errs = f.errs[1:]
		return nil, e
	}
	return f.samples, nil
}

type fakeWriter struct {
	desc  string
	errs  []error
	calls int
}

func (w *fakeWriter) UpdateDescription(_ context.Context, _ string, merge func(string) string) error {
	w.calls++
	if len(w.errs) > 0 {
		e := w.errs[0]
		w.errs = w.errs[1:]
		return e
	}
	w.desc = merge(w.desc)
	return nil
}

func readings() []stats.Sample {
	var out []stats.Sample
	for i, v := range []float64{100, 120, 140, 160} {
		out = append(out, stats.Sample{Time: start.Add(time.Duration(i) * 5 * time.Minute), Value: v})
	}
	return out
}

func activity() Activity {
	return Activity{StravaID: "42", Start: start, Duration: 20 * time.Minute}
}

func setup(src *fakeSource, w *fakeWriter) (*Queue, *memStore) {
	st := newStore()
	p := &Processor{Store: st, Source: src, SourceName: "dexcom", Writer: w}
	return NewQueue(p, []time.Duration{time.Millisecond, time.Millisecond}, 8), st
}

func runOne(t *testing.T, q *Queue, j Job) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.handle(ctx, j)
}

func TestSuccess(t *testing.T) {
	w := &fakeWriter{desc: "My run"}
	q, st := setup(&fakeSource{samples: readings()}, w)
	runOne(t, q, Job{Activity: activity()})

	a := st.acts["42"]
	if a.Status != StatusDone || a.Summary == nil || a.Summary.TIR != 100 {
		t.Fatalf("activity = %+v", a)
	}
	if !strings.HasPrefix(w.desc, "My run\n\n"+render.Prefix+"TIR 100%") {
		t.Errorf("description = %q", w.desc)
	}
	if len(st.samples) != 4 || len(st.events) != 0 {
		t.Errorf("samples=%d events=%d", len(st.samples), len(st.events))
	}
}

func TestDoneActivitySkippedUnlessForced(t *testing.T) {
	w := &fakeWriter{}
	q, st := setup(&fakeSource{samples: readings()}, w)
	st.acts["42"] = Activity{StravaID: "42", Status: StatusDone}

	runOne(t, q, Job{Activity: activity()})
	if w.calls != 0 || st.acts["42"].Status != StatusDone {
		t.Fatalf("done activity was reprocessed: calls=%d", w.calls)
	}
	runOne(t, q, Job{Activity: activity(), Force: true})
	if w.calls != 1 {
		t.Errorf("forced run calls = %d, want 1", w.calls)
	}
}

func TestRetryThenSuccess(t *testing.T) {
	w := &fakeWriter{errs: []error{errors.New("boom"), errors.New("boom")}}
	q, st := setup(&fakeSource{samples: readings()}, w)
	runOne(t, q, Job{Activity: activity()})

	if st.acts["42"].Status != StatusDone || w.calls != 3 {
		t.Errorf("status=%s calls=%d", st.acts["42"].Status, w.calls)
	}
	if len(st.events) != 0 {
		t.Errorf("no event expected after recovery, got %+v", st.events)
	}
}

func TestRetriesExhausted(t *testing.T) {
	w := &fakeWriter{errs: []error{errors.New("a"), errors.New("b"), errors.New("c")}}
	q, st := setup(&fakeSource{samples: readings()}, w)
	runOne(t, q, Job{Activity: activity()})

	a := st.acts["42"]
	if a.Status != StatusFailed || a.Attempts != 3 || w.calls != 3 {
		t.Errorf("status=%s attempts=%d calls=%d", a.Status, a.Attempts, w.calls)
	}
	if len(st.events) != 1 || st.events[0].Type != EventStravaFailed {
		t.Errorf("events = %+v", st.events)
	}
}

func TestSessionExpiredNotRetried(t *testing.T) {
	w := &fakeWriter{errs: []error{ErrSessionExpired}}
	q, st := setup(&fakeSource{samples: readings()}, w)
	runOne(t, q, Job{Activity: activity()})

	if w.calls != 1 || len(st.events) != 1 || st.events[0].Type != EventSessionExpired {
		t.Errorf("calls=%d events=%+v", w.calls, st.events)
	}
}

func TestNoDataRetriedThenReported(t *testing.T) {
	src := &fakeSource{}
	q, st := setup(src, &fakeWriter{})
	runOne(t, q, Job{Activity: activity()})

	if src.calls != 3 {
		t.Errorf("source calls = %d, want 3", src.calls)
	}
	if len(st.events) != 1 || st.events[0].Type != EventGlucoseUnavailable {
		t.Errorf("events = %+v", st.events)
	}
}

func TestTooOldFallsBackToStoredSamples(t *testing.T) {
	src := &fakeSource{errs: []error{glucose.ErrTooOld}}
	w := &fakeWriter{}
	q, st := setup(src, w)
	st.samples = readings()
	runOne(t, q, Job{Activity: activity()})

	if st.acts["42"].Status != StatusDone || w.calls != 1 {
		t.Errorf("status=%s calls=%d", st.acts["42"].Status, w.calls)
	}
}

func TestTooOldWithoutStoredDataFailsOnce(t *testing.T) {
	src := &fakeSource{errs: []error{glucose.ErrTooOld}}
	q, st := setup(src, &fakeWriter{})
	runOne(t, q, Job{Activity: activity()})

	if src.calls != 1 {
		t.Errorf("source calls = %d, want 1 (not retryable)", src.calls)
	}
	if len(st.events) != 1 || st.events[0].Type != EventGlucoseUnavailable {
		t.Errorf("events = %+v", st.events)
	}
}

func TestEnqueueDedupeAndRun(t *testing.T) {
	w := &fakeWriter{}
	q, st := setup(&fakeSource{samples: readings()}, w)

	if ok, err := q.Enqueue(Job{Activity: activity()}); !ok || err != nil {
		t.Fatalf("first enqueue = %v, %v", ok, err)
	}
	if ok, _ := q.Enqueue(Job{Activity: activity()}); ok {
		t.Fatal("duplicate enqueue accepted")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { q.Run(ctx); close(done) }()
	deadline := time.After(2 * time.Second)
	for {
		st.mu.Lock()
		s := st.acts["42"].Status
		st.mu.Unlock()
		if s == StatusDone {
			break
		}
		select {
		case <-deadline:
			t.Fatal("job did not finish")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done

	q.mu.Lock()
	n := len(q.pending)
	q.mu.Unlock()
	if n != 0 {
		t.Errorf("pending = %d after completion", n)
	}
}

func TestQueueFull(t *testing.T) {
	st := newStore()
	q := NewQueue(&Processor{Store: st}, nil, 1)
	a, b := activity(), activity()
	b.StravaID = "43"
	if _, err := q.Enqueue(Job{Activity: a}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(Job{Activity: b}); !errors.Is(err, ErrQueueFull) {
		t.Errorf("err = %v, want ErrQueueFull", err)
	}
}

func TestOriginalSavedBeforeWriteAndKeptOnReprocess(t *testing.T) {
	w := &fakeWriter{desc: "My run\nfelt good"}
	q, st := setup(&fakeSource{samples: readings()}, w)
	runOne(t, q, Job{Activity: activity()})

	a := st.acts["42"]
	if a.Original == nil || *a.Original != "My run\nfelt good" {
		t.Fatalf("original = %v", a.Original)
	}
	// Second run sees our block in the description. The backup must not change.
	runOne(t, q, Job{Activity: activity(), Force: true})
	if got := *st.acts["42"].Original; got != "My run\nfelt good" {
		t.Errorf("original after reprocess = %q", got)
	}
}

func TestEmptyOriginalIsStored(t *testing.T) {
	w := &fakeWriter{}
	q, st := setup(&fakeSource{samples: readings()}, w)
	runOne(t, q, Job{Activity: activity()})
	if o := st.acts["42"].Original; o == nil || *o != "" {
		t.Fatalf("original = %v, want pointer to empty string", o)
	}
}

func TestBackupFailureLeavesDescriptionUntouched(t *testing.T) {
	w := &fakeWriter{desc: "My run"}
	q, st := setup(&fakeSource{samples: readings()}, w)
	st.failSave = true
	runOne(t, q, Job{Activity: activity()})
	if w.desc != "My run" {
		t.Errorf("description changed without a backup: %q", w.desc)
	}
	if st.acts["42"].Status == StatusDone {
		t.Error("activity marked done")
	}
}

func TestRestore(t *testing.T) {
	w := &fakeWriter{desc: "My run"}
	q, st := setup(&fakeSource{samples: readings()}, w)
	runOne(t, q, Job{Activity: activity()})
	runOne(t, q, Job{Activity: st.acts["42"], Restore: true})
	if w.desc != "My run" {
		t.Errorf("description = %q", w.desc)
	}
	if st.acts["42"].Status != StatusSkipped {
		t.Errorf("status = %q", st.acts["42"].Status)
	}
}

func TestRestoreWithoutOriginalReportsEvent(t *testing.T) {
	w := &fakeWriter{desc: "keep"}
	q, st := setup(&fakeSource{}, w)
	runOne(t, q, Job{Activity: activity(), Restore: true})
	if w.desc != "keep" || w.calls != 0 || len(st.events) != 1 {
		t.Errorf("desc=%q calls=%d events=%d", w.desc, w.calls, len(st.events))
	}
}

// A merge that would damage the user's own text is refused before anything is
// written, and the failure says why.
func TestUnsafeMergeIsRefused(t *testing.T) {
	w := &fakeWriter{desc: "My run"}
	st := newStore()
	p := &Processor{Store: st, Source: &fakeSource{samples: readings()}, SourceName: "dexcom", Writer: w,
		merge: func(_, block string) string { return block }} // drops "My run"
	a := activity()
	err := p.Process(context.Background(), &a)
	if !errors.Is(err, ErrUnsafeMerge) {
		t.Fatalf("err = %v, want ErrUnsafeMerge", err)
	}
	if w.desc != "My run" {
		t.Errorf("description was changed to %q", w.desc)
	}
	if got := st.acts["42"]; got.Status == StatusDone {
		t.Errorf("activity marked done: %+v", got)
	}
}

func TestOnDoneCalledOnceForFirstRunOnly(t *testing.T) {
	w := &fakeWriter{desc: "My run"}
	q, _ := setup(&fakeSource{samples: readings()}, w)
	var got []Activity
	q.OnDone = func(_ context.Context, a Activity) { got = append(got, a) }

	runOne(t, q, Job{Activity: activity()})
	if len(got) != 1 || got[0].Summary == nil || got[0].StravaID != "42" {
		t.Fatalf("OnDone calls = %+v", got)
	}
	runOne(t, q, Job{Activity: activity()}) // already done: skipped
	runOne(t, q, Job{Activity: activity(), Force: true})
	if len(got) != 1 {
		t.Errorf("OnDone ran for a skipped or forced run: %d calls", len(got))
	}
}

func TestOnDoneNotCalledOnFailure(t *testing.T) {
	q, _ := setup(&fakeSource{}, &fakeWriter{})
	called := false
	q.OnDone = func(context.Context, Activity) { called = true }
	runOne(t, q, Job{Activity: activity()})
	if called {
		t.Error("OnDone ran for a failed activity")
	}
}

type photoWriter struct {
	fakeWriter
	photos [][]byte
	err    error
}

func (w *photoWriter) UploadPhoto(_ context.Context, _, _ string, png []byte) error {
	if w.err != nil {
		return w.err
	}
	w.photos = append(w.photos, png)
	return nil
}

func chartSetup(w Writer, on bool) (*Queue, *memStore) {
	st := newStore()
	st.set.ChartImage = on
	p := &Processor{Store: st, Source: &fakeSource{samples: readings()}, SourceName: "dexcom", Writer: w}
	return NewQueue(p, []time.Duration{time.Millisecond, time.Millisecond}, 8), st
}

func TestChartUploadedOnceAndOnlyWhenEnabled(t *testing.T) {
	w := &photoWriter{}
	q, st := chartSetup(w, true)
	runOne(t, q, Job{Activity: activity()})
	if len(w.photos) != 1 || !bytes.HasPrefix(w.photos[0], []byte("\x89PNG")) || !st.acts["42"].ChartUploaded {
		t.Fatalf("photos=%d uploaded=%v", len(w.photos), st.acts["42"].ChartUploaded)
	}
	// A reprocess must not attach a second copy.
	runOne(t, q, Job{Activity: activity(), Force: true})
	if len(w.photos) != 1 {
		t.Errorf("photos after reprocess = %d, want 1", len(w.photos))
	}

	off := &photoWriter{}
	q2, st2 := chartSetup(off, false)
	runOne(t, q2, Job{Activity: activity()})
	if len(off.photos) != 0 || st2.acts["42"].ChartUploaded {
		t.Error("chart uploaded although the setting is off")
	}
}

func TestChartFailureFailsTheRunButKeepsDescription(t *testing.T) {
	w := &photoWriter{err: errors.New("no file input")}
	w.desc = "My run"
	q, st := chartSetup(w, true)
	runOne(t, q, Job{Activity: activity()})
	if a := st.acts["42"]; a.Status != StatusFailed || a.ChartUploaded || !strings.Contains(a.Error, "upload chart") {
		t.Fatalf("activity = %+v", a)
	}
	if !strings.Contains(w.desc, render.Prefix) {
		t.Errorf("description lost: %q", w.desc)
	}
}

func TestChartSkippedForWriterWithoutPhotos(t *testing.T) {
	q, st := chartSetup(&fakeWriter{}, true)
	runOne(t, q, Job{Activity: activity()})
	if a := st.acts["42"]; a.Status != StatusDone || a.ChartUploaded {
		t.Errorf("activity = %+v", a)
	}
}

type permErr struct{}

func (permErr) Error() string   { return "no such element" }
func (permErr) Permanent() bool { return true }

func TestPermanentErrorIsNotRetried(t *testing.T) {
	w := &fakeWriter{errs: []error{permErr{}, permErr{}, permErr{}}}
	q, st := setup(&fakeSource{samples: readings()}, w)
	runOne(t, q, Job{Activity: activity()})
	if w.calls != 1 {
		t.Errorf("writer called %d times, want 1 (a missing element does not appear by waiting)", w.calls)
	}
	if a := st.acts["42"]; a.Status != StatusFailed || !strings.Contains(a.Error, "no such element") {
		t.Errorf("activity = %+v", a)
	}
}

func TestRetryExplainsItselfWhileWaiting(t *testing.T) {
	w := &fakeWriter{errs: []error{errors.New("chrome hiccup")}}
	q, st := setup(&fakeSource{samples: readings()}, w)
	var seen string
	q.Backoff = []time.Duration{50 * time.Millisecond}
	go func() {
		time.Sleep(20 * time.Millisecond)
		st.mu.Lock()
		seen = st.acts["42"].Error
		st.mu.Unlock()
	}()
	runOne(t, q, Job{Activity: activity()})
	if !strings.Contains(seen, "Attempt 1 failed") || !strings.Contains(seen, "chrome hiccup") {
		t.Errorf("while waiting, activity error = %q", seen)
	}
	if a := st.acts["42"]; a.Status != StatusDone || a.Error != "" {
		t.Errorf("after the retry: %+v", a)
	}
}

func TestRetryChartAttachesAgain(t *testing.T) {
	w := &photoWriter{}
	q, _ := chartSetup(w, true)
	runOne(t, q, Job{Activity: activity()})
	again := activity()
	again.RetryChart = true
	runOne(t, q, Job{Activity: again, Force: true})
	if len(w.photos) != 2 {
		t.Errorf("photos = %d, want 2 after an explicit retry", len(w.photos))
	}
}

type hrWriter struct {
	photoWriter
	hr   []chartimg.HRPoint
	err  error
	asks int
}

func (w *hrWriter) HeartRate(context.Context, string, time.Time) ([]chartimg.HRPoint, error) {
	w.asks++
	return w.hr, w.err
}

func TestHeartRateFetchedOnceForTheChart(t *testing.T) {
	w := &hrWriter{hr: []chartimg.HRPoint{{Time: start, BPM: 130}, {Time: start.Add(time.Minute), BPM: 140}}}
	q, st := chartSetup(w, true)
	st.set.ChartHR = true
	runOne(t, q, Job{Activity: activity()})
	if w.asks != 1 || len(st.acts["42"].HeartRate) != 2 || len(w.photos) != 1 {
		t.Errorf("asks=%d hr=%d photos=%d", w.asks, len(st.acts["42"].HeartRate), len(w.photos))
	}
	again := activity()
	again.RetryChart = true
	again.HeartRate = st.acts["42"].HeartRate
	runOne(t, q, Job{Activity: again, Force: true})
	if w.asks != 1 {
		t.Errorf("heart rate fetched again (%d asks)", w.asks)
	}
}

func TestHeartRateFailureDoesNotFailTheChart(t *testing.T) {
	w := &hrWriter{err: errors.New("streams down")}
	q, st := chartSetup(w, true)
	st.set.ChartHR = true
	runOne(t, q, Job{Activity: activity()})
	if a := st.acts["42"]; a.Status != StatusDone || !a.ChartUploaded || len(w.photos) != 1 {
		t.Errorf("activity = %+v photos=%d", a, len(w.photos))
	}
	off := &hrWriter{}
	q2, _ := chartSetup(off, true) // chart_hr off
	runOne(t, q2, Job{Activity: activity()})
	if off.asks != 0 {
		t.Error("heart rate fetched although chart_hr is off")
	}
}
