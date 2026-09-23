// Package canary runs a daily dry run against the Strava edit page, so a
// broken selector (Strava changed its markup) is caught by a notification
// before it silently fails a real activity.
package canary

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/strava"
)

// Inspector dry-runs the edit page without saving anything. *strava.Writer satisfies it.
type Inspector interface {
	Inspect(ctx context.Context, stravaID string, includeHTML bool) (*strava.Report, error)
}

// Store is the subset of jobs.Store the canary needs.
type Store interface {
	// ListActivities returns activities newest first; the canary checks the most recent.
	ListActivities(ctx context.Context, limit int) ([]jobs.Activity, error)
	RecordEvent(ctx context.Context, e jobs.Event) error
}

// Runner periodically checks that Strava's edit page still has a description
// field and a way to save, using the most recently processed activity.
type Runner struct {
	Inspector Inspector
	Store     Store
	Interval  func() time.Duration // read before every wait; default 24 hours

	mu         sync.Mutex // serializes Once against the background loop
	lastFailed bool       // avoid one notification per failed check while the same problem persists
}

// Once runs a single check. It returns nil when there is nothing to check yet
// (no activity has been processed) or the check passed.
func (r *Runner) Once(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	acts, err := r.Store.ListActivities(ctx, 1)
	if err != nil {
		return fmt.Errorf("canary: list activities: %w", err)
	}
	if len(acts) == 0 {
		return nil // nothing processed yet; nothing to check against
	}

	rep, err := r.Inspector.Inspect(ctx, acts[0].StravaID, false)
	if err != nil {
		return r.fail(ctx, acts[0].StravaID, fmt.Sprintf("could not open the edit page: %v", err))
	}
	switch {
	case !rep.LoggedIn:
		return r.fail(ctx, acts[0].StravaID, "Strava sent the browser to the login page: the stored session is no longer valid")
	case rep.DescriptionSelector == "":
		return r.fail(ctx, acts[0].StravaID, "the description field was not found with the configured selectors")
	case rep.SaveMethod == "":
		return r.fail(ctx, acts[0].StravaID, "no way to save the form was found")
	}

	r.lastFailed = false
	return nil
}

func (r *Runner) fail(ctx context.Context, stravaID, msg string) error {
	if !r.lastFailed {
		r.lastFailed = true
		if rerr := r.Store.RecordEvent(ctx, jobs.Event{
			Type: jobs.EventCanaryFailed, Severity: "error", StravaID: stravaID,
			Message: "canary check failed: " + msg,
		}); rerr != nil {
			log.Printf("canary: record event: %v", rerr)
		}
	}
	return errors.New("canary: " + msg)
}

// Run checks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	for {
		if err := r.Once(ctx); err != nil {
			log.Printf("canary: %v", err)
		}
		wait := 24 * time.Hour
		if r.Interval != nil {
			if d := r.Interval(); d > 0 {
				wait = d
			}
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
