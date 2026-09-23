package canary

import (
	"context"
	"errors"
	"testing"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/strava"
)

type fakeInspector struct {
	rep *strava.Report
	err error
	ids []string // stravaIDs Inspect was called with
}

func (f *fakeInspector) Inspect(_ context.Context, stravaID string, _ bool) (*strava.Report, error) {
	f.ids = append(f.ids, stravaID)
	return f.rep, f.err
}

type fakeStore struct {
	acts    []jobs.Activity
	listErr error
	events  []jobs.Event
}

func (f *fakeStore) ListActivities(context.Context, int) ([]jobs.Activity, error) {
	return f.acts, f.listErr
}
func (f *fakeStore) RecordEvent(_ context.Context, e jobs.Event) error {
	f.events = append(f.events, e)
	return nil
}

func TestOnceNothingToCheck(t *testing.T) {
	r := &Runner{Inspector: &fakeInspector{}, Store: &fakeStore{}}
	if err := r.Once(context.Background()); err != nil {
		t.Errorf("got %v, want nil (nothing processed yet)", err)
	}
}

func TestOncePasses(t *testing.T) {
	insp := &fakeInspector{rep: &strava.Report{LoggedIn: true, DescriptionSelector: "textarea", SaveMethod: "form-button"}}
	st := &fakeStore{acts: []jobs.Activity{{StravaID: "42"}}}
	r := &Runner{Inspector: insp, Store: st}
	if err := r.Once(context.Background()); err != nil {
		t.Errorf("got %v, want nil", err)
	}
	if len(insp.ids) != 1 || insp.ids[0] != "42" {
		t.Errorf("inspected ids = %v, want [42]", insp.ids)
	}
	if len(st.events) != 0 {
		t.Errorf("recorded events on a pass: %+v", st.events)
	}
}

func TestOnceFailsOnMissingSelector(t *testing.T) {
	insp := &fakeInspector{rep: &strava.Report{LoggedIn: true, DescriptionSelector: ""}}
	st := &fakeStore{acts: []jobs.Activity{{StravaID: "42"}}}
	r := &Runner{Inspector: insp, Store: st}
	if err := r.Once(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if len(st.events) != 1 || st.events[0].Type != jobs.EventCanaryFailed || st.events[0].StravaID != "42" {
		t.Errorf("events = %+v", st.events)
	}
}

func TestOnceFailsOnSessionExpired(t *testing.T) {
	insp := &fakeInspector{rep: &strava.Report{LoggedIn: false}}
	st := &fakeStore{acts: []jobs.Activity{{StravaID: "42"}}}
	r := &Runner{Inspector: insp, Store: st}
	if err := r.Once(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if len(st.events) != 1 {
		t.Fatalf("events = %+v", st.events)
	}
}

func TestOnceFailsOnInspectError(t *testing.T) {
	insp := &fakeInspector{err: errors.New("boom")}
	st := &fakeStore{acts: []jobs.Activity{{StravaID: "42"}}}
	r := &Runner{Inspector: insp, Store: st}
	if err := r.Once(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if len(st.events) != 1 {
		t.Fatalf("events = %+v", st.events)
	}
}

// TestFailureNotDoubleReported is a regression-shaped test: repeated failures
// (e.g. from the background loop) must not spam a notification every time,
// only when the state changes, same pattern as poll.Poller's sessionReported.
func TestFailureNotDoubleReported(t *testing.T) {
	insp := &fakeInspector{rep: &strava.Report{LoggedIn: true}}
	st := &fakeStore{acts: []jobs.Activity{{StravaID: "42"}}}
	r := &Runner{Inspector: insp, Store: st}

	_ = r.Once(context.Background())
	_ = r.Once(context.Background())
	_ = r.Once(context.Background())
	if len(st.events) != 1 {
		t.Errorf("events = %d, want 1 (deduped)", len(st.events))
	}

	// Recovery, then a fresh failure, reports again.
	insp.rep = &strava.Report{LoggedIn: true, DescriptionSelector: "textarea", SaveMethod: "form-button"}
	if err := r.Once(context.Background()); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	insp.rep = &strava.Report{LoggedIn: true}
	_ = r.Once(context.Background())
	if len(st.events) != 2 {
		t.Errorf("events = %d, want 2 (recovered then failed again)", len(st.events))
	}
}

func TestListActivitiesError(t *testing.T) {
	st := &fakeStore{listErr: errors.New("db down")}
	r := &Runner{Inspector: &fakeInspector{}, Store: st}
	if err := r.Once(context.Background()); err == nil {
		t.Error("expected an error")
	}
}
