// Package bus tells live web views that something changed. Publishes coalesce:
// a subscriber that has not read the last one yet gets a single wake-up.
package bus

import "sync"

// Bus fans one signal out to any number of subscribers.
type Bus struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

// Subscribe returns a channel that receives a value after each Publish, and a
// function that unsubscribes. Call it when the client goes away.
func (b *Bus) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	if b.subs == nil {
		b.subs = map[chan struct{}]struct{}{}
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// Publish wakes every subscriber. It never blocks.
func (b *Bus) Publish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
