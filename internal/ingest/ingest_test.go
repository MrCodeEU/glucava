package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/MrCodeEU/glucava/internal/metrics"
	"github.com/MrCodeEU/glucava/internal/stats"
)

type fakeSource struct {
	samples          []stats.Sample
	err              error
	calls            int
	lastFrom, lastTo time.Time
}

func (f *fakeSource) Samples(_ context.Context, from, to time.Time) ([]stats.Sample, error) {
	f.calls++
	f.lastFrom, f.lastTo = from, to
	return f.samples, f.err
}

type fakeStore struct {
	saved   []stats.Sample
	source  string
	saveErr error
}

func (f *fakeStore) SaveSamples(_ context.Context, source string, samples []stats.Sample) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.source = source
	f.saved = append(f.saved, samples...)
	return nil
}

func TestOnceStoresFetchedSamples(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{samples: []stats.Sample{{Time: now.Add(-5 * time.Minute), Value: 110}}}
	st := &fakeStore{}
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom", Now: func() time.Time { return now }}

	n, err := in.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(st.saved) != 1 || st.source != "dexcom" {
		t.Fatalf("n=%d saved=%v source=%q", n, st.saved, st.source)
	}
	// Default lookback is 20 minutes.
	if !src.lastFrom.Equal(now.Add(-20*time.Minute)) || !src.lastTo.Equal(now) {
		t.Errorf("window = [%v, %v]", src.lastFrom, src.lastTo)
	}
}

func TestOnceNoSamplesDoesNotCallStore(t *testing.T) {
	src := &fakeSource{}
	st := &fakeStore{}
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom"}

	n, err := in.Once(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if st.saved != nil {
		t.Errorf("store called with no samples: %v", st.saved)
	}
}

func TestOnceSourceErrorPropagates(t *testing.T) {
	wantErr := errors.New("source down")
	src := &fakeSource{err: wantErr}
	st := &fakeStore{}
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom"}

	_, err := in.Once(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wrapping %v", err, wantErr)
	}
}

func TestOnceStoreErrorPropagates(t *testing.T) {
	wantErr := errors.New("db down")
	src := &fakeSource{samples: []stats.Sample{{Time: time.Now(), Value: 100}}}
	st := &fakeStore{saveErr: wantErr}
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom"}

	_, err := in.Once(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wrapping %v", err, wantErr)
	}
}

func TestOnceWidensWindowWhenLatestIsStale(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{}
	st := &fakeStore{}
	// The newest stored reading is 5 hours old: the plain 20-minute lookback
	// would never reach it, so the fetch should widen to cover the gap
	// instead (the flight-mode scenario this exists for).
	last := now.Add(-5 * time.Hour)
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom", Now: func() time.Time { return now },
		Latest: func(context.Context) (time.Time, bool) { return last, true }}

	if _, err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !src.lastFrom.Equal(last) || !src.lastTo.Equal(now) {
		t.Errorf("window = [%v, %v], want [%v, %v]", src.lastFrom, src.lastTo, last, now)
	}
}

func TestOnceCapsWidenedWindowAtMaxLookback(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{}
	st := &fakeStore{}
	// Stale for 3 days: real gap far exceeds what the source can serve, so
	// the fetch must still be capped at MaxLookback, not the full gap (the
	// source would reject the wider request anyway).
	last := now.Add(-72 * time.Hour)
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom", Now: func() time.Time { return now },
		MaxLookback: 23*time.Hour + 50*time.Minute,
		Latest:      func(context.Context) (time.Time, bool) { return last, true }}

	if _, err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := now.Add(-(23*time.Hour + 50*time.Minute))
	if !src.lastFrom.Equal(want) {
		t.Errorf("window from = %v, want %v (capped)", src.lastFrom, want)
	}
}

func TestOnceKeepsPlainLookbackWhenFresh(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{}
	st := &fakeStore{}
	// The newest stored reading is within the plain lookback already: no
	// widening should happen on an ordinary tick.
	last := now.Add(-5 * time.Minute)
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom", Now: func() time.Time { return now },
		Latest: func(context.Context) (time.Time, bool) { return last, true }}

	if _, err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !src.lastFrom.Equal(now.Add(-20 * time.Minute)) {
		t.Errorf("window from = %v, want the plain 20-minute lookback", src.lastFrom)
	}
}

func TestOnceIgnoresLatestWhenNoReadingEverArrived(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{}
	st := &fakeStore{}
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom", Now: func() time.Time { return now },
		Latest: func(context.Context) (time.Time, bool) { return time.Time{}, false }}

	if _, err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !src.lastFrom.Equal(now.Add(-20 * time.Minute)) {
		t.Errorf("window from = %v, want the plain 20-minute lookback", src.lastFrom)
	}
}

func TestForceOnceUsesMaxLookbackRegardlessOfLatest(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{}
	st := &fakeStore{}
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom", Now: func() time.Time { return now }}

	if _, err := in.ForceOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := now.Add(-(23*time.Hour + 50*time.Minute))
	if !src.lastFrom.Equal(want) || !src.lastTo.Equal(now) {
		t.Errorf("window = [%v, %v], want [%v, %v]", src.lastFrom, src.lastTo, want, now)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	src := &fakeSource{}
	st := &fakeStore{}
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom", Interval: 5 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { in.Run(ctx); close(done) }()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	if src.calls == 0 {
		t.Error("Run never ticked")
	}
}

// TestOnceUpdatesMetricsOnSuccess and TestOnceUpdatesMetricsOnError guard
// the metrics wiring in fetchAndStore.
func TestOnceUpdatesMetricsOnSuccess(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{samples: []stats.Sample{{Time: now.Add(-5 * time.Minute), Value: 110}}}
	st := &fakeStore{}
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom", Now: func() time.Time { return now }}

	storedBefore := testutil.ToFloat64(metrics.IngestReadingsStoredTotal)
	if _, err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := testutil.ToFloat64(metrics.IngestReadingsStoredTotal); got != storedBefore+1 {
		t.Errorf("IngestReadingsStoredTotal = %v, want %v", got, storedBefore+1)
	}
	if got := testutil.ToFloat64(metrics.IngestLastSuccessTimestamp); got != float64(now.Unix()) {
		t.Errorf("IngestLastSuccessTimestamp = %v, want %v", got, now.Unix())
	}
}

func TestOnceUpdatesMetricsOnError(t *testing.T) {
	src := &fakeSource{err: errors.New("source down")}
	st := &fakeStore{}
	in := &Ingestor{Source: src, Store: st, SourceName: "dexcom"}

	errsBefore := testutil.ToFloat64(metrics.IngestErrorsTotal)
	if _, err := in.Once(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if got := testutil.ToFloat64(metrics.IngestErrorsTotal); got != errsBefore+1 {
		t.Errorf("IngestErrorsTotal = %v, want %v", got, errsBefore+1)
	}
}
