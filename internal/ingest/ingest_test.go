package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

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
