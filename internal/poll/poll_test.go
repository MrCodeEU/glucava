package poll

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

var now = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

type fakeLister struct {
	acts  []jobs.Activity
	err   error
	calls atomic.Int32
}

func (f *fakeLister) ListRecent(context.Context, int) ([]jobs.Activity, error) {
	f.calls.Add(1)
	return f.acts, f.err
}

type fakeQueue struct{ got []jobs.Job }

func (q *fakeQueue) Enqueue(j jobs.Job) (bool, error) { q.got = append(q.got, j); return true, nil }

type fakeStore struct {
	known  map[string]jobs.Activity
	events []jobs.Event
}

func (s *fakeStore) Settings(context.Context) (jobs.Settings, error) {
	return jobs.Settings{Unit: render.MgDL, Range: stats.DefaultRange, Post: 30 * time.Minute}, nil
}
func (s *fakeStore) Activity(_ context.Context, id string) (*jobs.Activity, error) {
	if a, ok := s.known[id]; ok {
		return &a, nil
	}
	return nil, nil
}
func (s *fakeStore) SaveActivity(context.Context, *jobs.Activity) error        { return nil }
func (s *fakeStore) SaveSamples(context.Context, string, []stats.Sample) error { return nil }
func (s *fakeStore) LoadSamples(context.Context, string, time.Time, time.Time) ([]stats.Sample, error) {
	return nil, nil
}
func (s *fakeStore) RecordEvent(_ context.Context, e jobs.Event) error {
	s.events = append(s.events, e)
	return nil
}

// act returns an activity that ended endedAgo before now.
func act(id string, endedAgo time.Duration) jobs.Activity {
	d := 45 * time.Minute
	return jobs.Activity{StravaID: id, Start: now.Add(-endedAgo - d), Duration: d}
}

func newPoller(l *fakeLister, q *fakeQueue, s *fakeStore) *Poller {
	return &Poller{Lister: l, Queue: q, Store: s, Now: func() time.Time { return now }}
}

func TestQueuesOnlyNewFinishedRecentActivities(t *testing.T) {
	l := &fakeLister{acts: []jobs.Activity{
		act("new", 2*time.Hour),          // queued
		act("done", 2*time.Hour),         // already processed
		act("failed", 2*time.Hour),       // failed earlier: left alone
		act("too-fresh", 10*time.Minute), // glucose window (30 min) not complete
		act("old", 30*time.Hour),         // older than the source keeps
	}}
	s := &fakeStore{known: map[string]jobs.Activity{
		"done": {Status: jobs.StatusDone}, "failed": {Status: jobs.StatusFailed},
	}}
	q := &fakeQueue{}

	n, err := newPoller(l, q, s).Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(q.got) != 1 || q.got[0].Activity.StravaID != "new" {
		t.Errorf("queued %d: %+v", n, q.got)
	}
}

func TestFreshActivityQueuedOnLaterPoll(t *testing.T) {
	l := &fakeLister{acts: []jobs.Activity{act("a", 10*time.Minute)}}
	q := &fakeQueue{}
	p := newPoller(l, q, &fakeStore{})

	if n, _ := p.Once(context.Background()); n != 0 {
		t.Fatalf("queued %d too early", n)
	}
	now = now.Add(25 * time.Minute)
	defer func() { now = now.Add(-25 * time.Minute) }()
	if n, _ := p.Once(context.Background()); n != 1 {
		t.Errorf("queued %d after the window closed, want 1", n)
	}
}

func TestSessionExpiredRecordsEvent(t *testing.T) {
	s := &fakeStore{}
	_, err := newPoller(&fakeLister{err: jobs.ErrSessionExpired}, &fakeQueue{}, s).Once(context.Background())
	if !errors.Is(err, jobs.ErrSessionExpired) {
		t.Fatalf("err = %v", err)
	}
	if len(s.events) != 1 || s.events[0].Type != jobs.EventSessionExpired {
		t.Errorf("events = %+v", s.events)
	}
}

func TestSessionExpiredReportedOnceUntilRecovered(t *testing.T) {
	s := &fakeStore{}
	l := &fakeLister{err: jobs.ErrSessionExpired}
	p := newPoller(l, &fakeQueue{}, s)
	for range 3 {
		_, _ = p.Once(context.Background())
	}
	if len(s.events) != 1 {
		t.Fatalf("events = %d, want 1", len(s.events))
	}
	l.err = nil // cookies fixed
	_, _ = p.Once(context.Background())
	l.err = jobs.ErrSessionExpired
	_, _ = p.Once(context.Background())
	if len(s.events) != 2 {
		t.Errorf("events = %d, want 2 after recovery and a new expiry", len(s.events))
	}
}

func TestOtherListErrorNoEvent(t *testing.T) {
	s := &fakeStore{}
	if _, err := newPoller(&fakeLister{err: errors.New("net down")}, &fakeQueue{}, s).Once(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if len(s.events) != 0 {
		t.Errorf("unexpected events: %+v", s.events)
	}
}

func TestRunPollsOnSignalAndStops(t *testing.T) {
	l := &fakeLister{}
	sig := make(chan struct{}, 1)
	p := newPoller(l, &fakeQueue{}, &fakeStore{})
	p.Signal = sig
	p.Interval = func() time.Duration { return time.Hour }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	waitCalls := func(n int) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for int(l.calls.Load()) < n {
			select {
			case <-deadline:
				t.Fatalf("calls = %d, want %d", l.calls.Load(), n)
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	waitCalls(1) // immediate first poll
	sig <- struct{}{}
	waitCalls(2) // signal wakes the hour-long wait
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}
