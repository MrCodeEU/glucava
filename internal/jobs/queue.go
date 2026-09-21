package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/glucose"
)

// DefaultBackoff is the wait before retry 1, 2 and 3.
var DefaultBackoff = []time.Duration{time.Minute, 3 * time.Minute, 10 * time.Minute}

// Queue runs jobs one at a time. A single worker keeps the browser session single-threaded.
type Queue struct {
	P       *Processor
	Backoff []time.Duration // len(Backoff) retries after the first attempt

	in      chan Job
	mu      sync.Mutex
	pending map[string]bool
}

// Job is one queued activity.
type Job struct {
	Activity Activity
	Force    bool
}

// NewQueue returns a Queue with room for size waiting jobs.
func NewQueue(p *Processor, backoff []time.Duration, size int) *Queue {
	return &Queue{P: p, Backoff: backoff, in: make(chan Job, size), pending: map[string]bool{}}
}

// Enqueue adds a job. It returns false if the activity is already queued or running.
func (q *Queue) Enqueue(j Job) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pending[j.Activity.StravaID] {
		return false, nil
	}
	select {
	case q.in <- j:
		q.pending[j.Activity.StravaID] = true
		return true, nil
	default:
		return false, ErrQueueFull
	}
}

// Run processes jobs until ctx is cancelled.
func (q *Queue) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-q.in:
			q.handle(ctx, j)
			q.mu.Lock()
			delete(q.pending, j.Activity.StravaID)
			q.mu.Unlock()
		}
	}
}

func (q *Queue) handle(ctx context.Context, j Job) {
	a := j.Activity
	if !j.Force {
		// Check before marking the activity as processing, which would hide a done state.
		if prev, err := q.P.Store.Activity(ctx, a.StravaID); err == nil && prev != nil && prev.Status == StatusDone {
			return
		}
	}
	a.Status = StatusProcessing
	if err := q.P.Store.SaveActivity(ctx, &a); err != nil {
		log.Printf("jobs: save activity %s: %v", a.StravaID, err)
	}

	var err error
	for attempt := 0; ; attempt++ {
		a.Attempts = attempt + 1
		err = q.P.Process(ctx, &a)
		if err == nil || !retryable(err) || attempt >= len(q.Backoff) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(q.Backoff[attempt]):
		}
	}
	if err == nil {
		return
	}

	typ, sev := classify(err)
	a.Status = StatusFailed
	a.Error = err.Error()
	if serr := q.P.Store.SaveActivity(ctx, &a); serr != nil {
		log.Printf("jobs: save activity %s: %v", a.StravaID, serr)
	}
	ev := Event{Type: typ, Severity: sev, StravaID: a.StravaID,
		Message: fmt.Sprintf("activity %s: %v", a.StravaID, err)}
	if rerr := q.P.Store.RecordEvent(ctx, ev); rerr != nil {
		log.Printf("jobs: record event: %v", rerr)
	}
}

func retryable(err error) bool {
	return !errors.Is(err, glucose.ErrAuth) &&
		!errors.Is(err, glucose.ErrTooOld) &&
		!errors.Is(err, ErrSessionExpired) &&
		!errors.Is(err, context.Canceled)
}

func classify(err error) (typ, severity string) {
	switch {
	case errors.Is(err, ErrSessionExpired):
		return EventSessionExpired, "error"
	case errors.Is(err, ErrNoData), errors.Is(err, glucose.ErrTooOld), errors.Is(err, glucose.ErrAuth):
		return EventGlucoseUnavailable, "error"
	default:
		return EventStravaFailed, "error"
	}
}
