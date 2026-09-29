// Package logging wraps an slog.Handler with an in-memory ring buffer of
// recent log entries, so the web UI can show a live log view without
// needing `docker logs` on the host. It wakes an internal/bus.Bus after each
// entry, the same "wake subscribers, they re-fetch and re-render" pattern
// already used for the dashboard's and an activity's live updates.
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/bus"
)

// Entry is one captured log line, with its attributes already formatted for
// display rather than kept as structured values, since the only consumer is
// a plain-text log view.
type Entry struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   string // "key=value key2=value2", empty if there were none
}

// DefaultCapacity is how many recent entries Handler keeps by default.
const DefaultCapacity = 1000

type state struct {
	mu  sync.Mutex
	buf []Entry
	cap int
	bus *bus.Bus
}

// Handler is an slog.Handler that both forwards to next (the real output,
// e.g. stderr) and appends to a shared in-memory ring buffer.
type Handler struct {
	next slog.Handler
	st   *state
}

// NewHandler wraps next, keeping up to capacity recent entries and calling
// b.Publish() (if b is not nil) after each one.
func NewHandler(next slog.Handler, capacity int, b *bus.Bus) *Handler {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Handler{next: next, st: &state{cap: capacity, bus: b}}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	var sb strings.Builder
	r.Attrs(func(a slog.Attr) bool {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%s=%v", a.Key, a.Value.Any())
		return true
	})

	h.st.mu.Lock()
	h.st.buf = append(h.st.buf, Entry{Time: r.Time, Level: r.Level, Message: r.Message, Attrs: sb.String()})
	if len(h.st.buf) > h.st.cap {
		h.st.buf = h.st.buf[len(h.st.buf)-h.st.cap:]
	}
	h.st.mu.Unlock()
	if h.st.bus != nil {
		h.st.bus.Publish()
	}

	return h.next.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs), st: h.st}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), st: h.st}
}

// Recent returns up to n of the newest entries, newest first. n <= 0 means
// every entry currently kept.
func (h *Handler) Recent(n int) []Entry {
	h.st.mu.Lock()
	defer h.st.mu.Unlock()
	if n <= 0 || n > len(h.st.buf) {
		n = len(h.st.buf)
	}
	out := make([]Entry, n)
	for i := 0; i < n; i++ {
		out[i] = h.st.buf[len(h.st.buf)-1-i]
	}
	return out
}
