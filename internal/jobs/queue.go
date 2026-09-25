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
	// OnDone, if set, is called after an activity was annotated for the first
	// time. A forced reprocess does not call it.
	OnDone func(ctx context.Context, a Activity)

	in      chan Job
	mu      sync.Mutex
	pending map[string]bool
}

// Job is one queued activity.
type Job struct {
	Activity Activity
	Force    bool
	Restore  bool // put the original description back instead of annotating
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
	if j.Restore {
		q.restore(ctx, a)
		return
	}
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
		// Say why, and when it will try again, instead of sitting silent for
		// minutes: the activity page shows this text while the job waits.
		log.Printf("jobs: activity %s: attempt %d failed, retrying in %s: %v", a.StravaID, attempt+1, q.Backoff[attempt], err)
		a.Error = fmt.Sprintf("Attempt %d failed: %v. Trying again in %s.", attempt+1, err, q.Backoff[attempt].Round(time.Second))
		if serr := q.P.Store.SaveActivity(ctx, &a); serr != nil {
			log.Printf("jobs: save activity %s: %v", a.StravaID, serr)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(q.Backoff[attempt]):
		}
	}
	if err == nil {
		log.Printf("jobs: activity %s: done (attempt %d)", a.StravaID, a.Attempts)
		if q.OnDone != nil && !j.Force {
			q.OnDone(ctx, a)
		}
		return
	}

	log.Printf("jobs: activity %s: failed after %d attempt(s): %v", a.StravaID, a.Attempts, err)
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

// restore undoes the edit once. It does not retry, and reports a failure as an event.
func (q *Queue) restore(ctx context.Context, a Activity) {
	err := q.P.Restore(ctx, &a)
	if err == nil {
		return
	}
	typ, sev := classify(err)
	ev := Event{Type: typ, Severity: sev, StravaID: a.StravaID,
		Message: fmt.Sprintf("restore activity %s: %v", a.StravaID, err)}
	if rerr := q.P.Store.RecordEvent(ctx, ev); rerr != nil {
		log.Printf("jobs: record event: %v", rerr)
	}
}

// permanent is implemented by errors that a second try cannot fix, such as a
// page element that is not there.
type permanent interface{ Permanent() bool }

func retryable(err error) bool {
	var p permanent
	if errors.As(err, &p) && p.Permanent() {
		return false
	}
	return !errors.Is(err, ErrUnsafeMerge) && !errors.Is(err, ErrNoOriginal) &&
		!errors.Is(err, glucose.ErrAuth) &&
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
