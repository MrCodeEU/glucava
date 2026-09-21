// Package trigger serves the authenticated endpoint that phones call when Strava
// posts a new activity. It only asks the poller to check now; it carries no data.
package trigger

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
)

// Signal is a coalescing wake-up for the poller. Many kicks before the poller
// wakes count as one.
type Signal struct{ c chan struct{} }

// NewSignal returns a Signal.
func NewSignal() *Signal { return &Signal{c: make(chan struct{}, 1)} }

// Kick requests a poll. It returns false if one is already pending.
func (s *Signal) Kick() bool {
	select {
	case s.c <- struct{}{}:
		return true
	default:
		return false
	}
}

// C returns the channel the poller selects on.
func (s *Signal) C() <-chan struct{} { return s.c }

// Verifier checks a bearer token.
type Verifier interface {
	Verify(token string) (bool, error)
}

// EventSink records rejected attempts for the notification outbox.
type EventSink interface {
	RecordEvent(ctx context.Context, e jobs.Event) error
}

// Handler is the POST /api/trigger endpoint.
type Handler struct {
	Tokens Verifier
	Signal *Signal
	Events EventSink // optional

	// ClientIP extracts the caller address. It defaults to the connection address.
	// Behind a reverse proxy, set it to read the proxy's forwarded address, or all
	// callers share one rate limit bucket.
	ClientIP func(*http.Request) string

	MaxFailures int              // failed attempts allowed per window and address; default 10
	Window      time.Duration    // default 1 minute
	Now         func() time.Time // for tests

	mu   sync.Mutex
	fail map[string]*bucket
}

type bucket struct {
	start    time.Time
	count    int
	reported bool
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Handler) params() (int, time.Duration) {
	max, win := h.MaxFailures, h.Window
	if max <= 0 {
		max = 10
	}
	if win <= 0 {
		win = time.Minute
	}
	return max, win
}

func (h *Handler) clientIP(r *http.Request) string {
	if h.ClientIP != nil {
		return h.ClientIP(r)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	ip := h.clientIP(r)
	if h.blocked(ip) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, "too_many_attempts")
		return
	}

	token, ok := bearer(r)
	valid := false
	if ok {
		var err error
		if valid, err = h.Tokens.Verify(token); err != nil {
			writeJSON(w, http.StatusInternalServerError, "error")
			return
		}
	}
	if !valid {
		reason := "invalid token"
		if !ok {
			reason = "missing token"
		}
		h.reject(r.Context(), ip, reason)
		writeJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if h.Signal.Kick() {
		writeJSON(w, http.StatusAccepted, "queued")
		return
	}
	writeJSON(w, http.StatusAccepted, "already_pending")
}

func (h *Handler) blocked(ip string) bool {
	max, win := h.params()
	h.mu.Lock()
	defer h.mu.Unlock()
	b := h.fail[ip]
	return b != nil && h.now().Sub(b.start) < win && b.count >= max
}

// reject counts a failed attempt and reports the first one per window as an event.
func (h *Handler) reject(ctx context.Context, ip, reason string) {
	_, win := h.params()
	h.mu.Lock()
	if h.fail == nil {
		h.fail = map[string]*bucket{}
	}
	b := h.fail[ip]
	if b == nil || h.now().Sub(b.start) >= win {
		b = &bucket{start: h.now()}
		h.fail[ip] = b
	}
	b.count++
	report := !b.reported
	b.reported = true
	h.mu.Unlock()

	if report && h.Events != nil {
		_ = h.Events.RecordEvent(ctx, jobs.Event{
			Type:     "trigger_rejected",
			Severity: "warning",
			Message:  fmt.Sprintf("trigger call from %s rejected: %s", ip, reason),
		})
	}
}

func bearer(r *http.Request) (string, bool) {
	v := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(v) <= len(p) || !strings.EqualFold(v[:len(p)], p) {
		return "", false
	}
	return strings.TrimSpace(v[len(p):]), true
}

func writeJSON(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}
