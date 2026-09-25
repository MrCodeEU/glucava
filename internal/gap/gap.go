// Package gap raises one alert when no glucose reading has arrived for too
// long, for example because the sensor app stopped sharing, the Dexcom login
// broke or the phone is off. It looks at stored readings, so it works for any
// glucose source and is independent of Strava activities.
package gap

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
)

// Monitor checks the age of the newest stored reading.
type Monitor struct {
	Latest    func(ctx context.Context) (time.Time, bool)
	Threshold func() time.Duration // 0 or less turns the check off
	Record    func(ctx context.Context, e jobs.Event) error
	Loc       func() *time.Location
	Now       func() time.Time
	Every     time.Duration // default 10 minutes

	mu      sync.Mutex
	alerted bool // one alert per gap; cleared when readings come back
}

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Once checks and, if a gap just started to exceed the threshold, records an
// event. It returns whether it did.
func (m *Monitor) Once(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	limit := m.Threshold()
	if limit <= 0 {
		m.alerted = false
		return false, nil
	}
	last, ok := m.Latest(ctx)
	if !ok {
		return false, nil // no reading has ever arrived: a fresh install, not a gap
	}
	age := m.now().Sub(last)
	if age <= limit {
		m.alerted = false
		return false, nil
	}
	if m.alerted {
		return false, nil
	}
	loc := time.Local
	if m.Loc != nil {
		loc = m.Loc()
	}
	err := m.Record(ctx, jobs.Event{
		Type: jobs.EventGlucoseGap, Severity: "warning",
		Message: fmt.Sprintf("no glucose reading for %s (the last one was at %s)", human(age), last.In(loc).Format("Mon 2 Jan, 15:04")),
	})
	if err != nil {
		return false, err // not marked as alerted: try again next time
	}
	m.alerted = true
	return true, nil
}

func human(d time.Duration) string {
	d = d.Round(time.Minute)
	if h := int(d.Hours()); h > 0 {
		return fmt.Sprintf("%dh %02dm", h, int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// Run checks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	every := m.Every
	if every <= 0 {
		every = 10 * time.Minute
	}
	for {
		if _, err := m.Once(ctx); err != nil {
			log.Printf("gap: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
