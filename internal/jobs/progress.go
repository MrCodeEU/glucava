package jobs

import "sync"

// Progress tracks the current step of an in-flight job, keyed by Strava
// activity id, for the live activity page. It is in-memory only, not
// persisted: a step is only meaningful while Process is actually running,
// and is cleared once it stops (done, failed, or the process restarts).
type Progress struct {
	mu    sync.Mutex
	steps map[string]string
}

// NewProgress returns an empty Progress tracker.
func NewProgress() *Progress {
	return &Progress{steps: map[string]string{}}
}

// Set records the current step for id. Exported so tests outside this
// package can simulate a job in flight without a real Processor.
func (p *Progress) Set(id, step string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps[id] = step
}

func (p *Progress) clear(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.steps, id)
}

// Step returns the current step for id, or "" if no job is running for it.
func (p *Progress) Step(id string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.steps[id]
}
