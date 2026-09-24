package notify

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// OutboxEvent is a stored event that has not been delivered yet.
type OutboxEvent struct {
	ID       string
	Type     string
	Severity string
	Message  string
	StravaID string
	Repaired bool
	Created  time.Time
}

// Outbox is the source of undelivered events.
type Outbox interface {
	Pending(ctx context.Context) ([]OutboxEvent, error)
	MarkNotified(ctx context.Context, id string) error
}

// Dispatcher delivers outbox events to every channel.
//
// An event counts as delivered when at least one channel accepts it. If all
// channels fail, it stays pending and is retried on the next flush, until it is
// older than MaxAge. An event with the same type and activity as one sent within
// Cooldown is marked notified without sending, so a repeating failure sends one
// message per cooldown period.
type Dispatcher struct {
	Outbox   Outbox
	Channels func() []Channel // read at each flush, so settings changes apply at once
	Cooldown time.Duration    // default 6 hours
	MaxAge   time.Duration    // default 24 hours
	Now      func() time.Time
	Link     func(Message) (href, label string) // optional: adds a web UI link to each message

	mu   sync.Mutex
	sent map[string]time.Time
}

func (d *Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Flush delivers pending events once. It returns the first storage error.
func (d *Dispatcher) Flush(ctx context.Context) error {
	cool, maxAge := d.Cooldown, d.MaxAge
	if cool <= 0 {
		cool = 6 * time.Hour
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}

	events, err := d.Outbox.Pending(ctx)
	if err != nil {
		return err
	}
	channels := d.Channels()

	for _, e := range events {
		now := d.now()
		key := e.Type + "|" + e.StravaID

		if now.Sub(e.Created) > maxAge || d.recentlySent(key, now, cool) {
			if err := d.Outbox.MarkNotified(ctx, e.ID); err != nil {
				return err
			}
			continue
		}
		if len(channels) == 0 {
			continue // nothing configured yet; keep the event until a channel exists
		}

		msg := Message{
			Type: e.Type, Severity: e.Severity, Title: Title(e.Type), Body: e.Message,
			StravaID: e.StravaID, Repaired: e.Repaired, Time: e.Created,
		}
		if d.Link != nil {
			msg.Link, msg.LinkLabel = d.Link(msg)
		}
		var errs []error
		delivered := false
		for _, c := range channels {
			if err := c.Send(ctx, msg); err != nil {
				log.Printf("notify: %s: %v", c.Name(), err)
				errs = append(errs, err)
				continue
			}
			delivered = true
		}
		if !delivered {
			log.Printf("notify: event %s not delivered: %v", e.ID, errors.Join(errs...))
			continue
		}
		d.remember(key, now)
		if err := d.Outbox.MarkNotified(ctx, e.ID); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dispatcher) recentlySent(key string, now time.Time, cool time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.sent[key]
	return ok && now.Sub(t) < cool
}

func (d *Dispatcher) remember(key string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sent == nil {
		d.sent = map[string]time.Time{}
	}
	d.sent[key] = now
}

// Run flushes every interval until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := d.Flush(ctx); err != nil && ctx.Err() == nil {
			log.Printf("notify: flush: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
