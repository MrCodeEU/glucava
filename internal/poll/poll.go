// Package poll finds new Strava activities and hands them to the job queue.
package poll

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
)

// Lister returns the newest activities, newest first.
type Lister interface {
	ListRecent(ctx context.Context, limit int) ([]jobs.Activity, error)
}

// Enqueuer accepts jobs. *jobs.Queue satisfies it.
type Enqueuer interface {
	Enqueue(j jobs.Job) (bool, error)
}

// Poller checks Strava on an interval and whenever Signal fires.
//
// An activity is queued when all of these hold:
//   - it ended less than MaxAge ago (the glucose source keeps only a day),
//   - its glucose window is complete, meaning end + post window has passed,
//   - the store has no record of it. Activities that are done, failed, pending or
//     processing are left alone; reprocess a failed one by hand.
type Poller struct {
	Lister   Lister
	Queue    Enqueuer
	Store    jobs.Store
	Signal   <-chan struct{}      // optional wake-up, e.g. from the trigger endpoint
	Interval func() time.Duration // read before every wait; default 10 minutes
	Limit    int                  // activities to fetch; default 20
	MaxAge   time.Duration        // default 24 hours
	Now      func() time.Time

	sessionReported bool // an expired session was reported and no poll has succeeded since
}

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Once runs one poll and returns how many activities it queued.
func (p *Poller) Once(ctx context.Context) (int, error) {
	limit, maxAge := p.Limit, p.MaxAge
	if limit <= 0 {
		limit = 20
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}

	acts, err := p.Lister.ListRecent(ctx, limit)
	if err != nil {
		if errors.Is(err, jobs.ErrSessionExpired) && !p.sessionReported {
			p.sessionReported = true
			_ = p.Store.RecordEvent(ctx, jobs.Event{
				Type: jobs.EventSessionExpired, Severity: "error",
				Message: "polling failed: the Strava session has expired, import fresh cookies",
			})
		}
		return 0, fmt.Errorf("list activities: %w", err)
	}
	p.sessionReported = false
	set, err := p.Store.Settings(ctx)
	if err != nil {
		return 0, fmt.Errorf("load settings: %w", err)
	}

	now := p.now()
	queued := 0
	for _, a := range acts {
		end := a.End()
		if now.Sub(end) > maxAge {
			continue
		}
		if now.Before(end.Add(set.Post)) {
			continue // glucose window not complete yet; the next poll picks it up
		}
		prev, err := p.Store.Activity(ctx, a.StravaID)
		if err != nil {
			return queued, fmt.Errorf("look up %s: %w", a.StravaID, err)
		}
		if prev != nil {
			continue
		}
		ok, err := p.Queue.Enqueue(jobs.Job{Activity: a})
		if err != nil {
			return queued, fmt.Errorf("enqueue %s: %w", a.StravaID, err)
		}
		if ok {
			queued++
		}
	}
	return queued, nil
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	for {
		if n, err := p.Once(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("poll: %v", err)
		} else if n > 0 {
			log.Printf("poll: queued %d new activities", n)
		}

		wait := 10 * time.Minute
		if p.Interval != nil {
			if d := p.Interval(); d > 0 {
				wait = d
			}
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-p.Signal:
			t.Stop()
		case <-t.C:
		}
	}
}
