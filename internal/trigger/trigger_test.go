package trigger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
)

type fakeTokens map[string]bool

func (f fakeTokens) Verify(t string) (bool, error) { return f[t], nil }

type sink struct{ events []jobs.Event }

func (s *sink) RecordEvent(_ context.Context, e jobs.Event) error {
	s.events = append(s.events, e)
	return nil
}

func newHandler() (*Handler, *sink, *time.Time) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	s := &sink{}
	return &Handler{
		Tokens:      fakeTokens{"gst_good": true},
		Signal:      NewSignal(),
		Events:      s,
		MaxFailures: 3,
		Now:         func() time.Time { return now },
	}, s, &now
}

func call(h *Handler, method, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/trigger", strings.NewReader(`{"any":"body"}`))
	req.RemoteAddr = "203.0.113.5:4444"
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestValidTokenKicksAndCoalesces(t *testing.T) {
	h, _, _ := newHandler()

	rec := call(h, http.MethodPost, "Bearer gst_good")
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "queued") {
		t.Fatalf("first = %d %s", rec.Code, rec.Body)
	}
	select {
	case <-h.Signal.C():
	default:
		t.Fatal("signal not set")
	}

	call(h, http.MethodPost, "Bearer gst_good") // sets it again
	rec = call(h, http.MethodPost, "Bearer gst_good")
	if !strings.Contains(rec.Body.String(), "already_pending") {
		t.Errorf("second = %s", rec.Body)
	}
}

func TestRejects(t *testing.T) {
	h, _, _ := newHandler()
	h.MaxFailures = 10
	for name, auth := range map[string]string{
		"missing": "", "wrong": "Bearer gst_bad", "scheme": "Basic gst_good", "empty": "Bearer ",
	} {
		if rec := call(h, http.MethodPost, auth); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: code = %d", name, rec.Code)
		}
	}
	if len(h.Signal.c) != 0 {
		t.Error("rejected call kicked the poller")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h, _, _ := newHandler()
	rec := call(h, http.MethodGet, "Bearer gst_good")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Errorf("code = %d allow = %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestRateLimitAndEventOncePerWindow(t *testing.T) {
	h, s, now := newHandler()

	for range 3 {
		if rec := call(h, http.MethodPost, "Bearer gst_bad"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d", rec.Code)
		}
	}
	if len(s.events) != 1 || s.events[0].Type != "trigger_rejected" {
		t.Errorf("events = %+v, want exactly one", s.events)
	}
	if strings.Contains(s.events[0].Message, "gst_bad") {
		t.Error("event message leaks the token")
	}

	// Blocked now, even for a valid token.
	rec := call(h, http.MethodPost, "Bearer gst_good")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("blocked = %d", rec.Code)
	}

	// Window passes: the address may try again, and a new window reports again.
	*now = now.Add(2 * time.Minute)
	if rec := call(h, http.MethodPost, "Bearer gst_good"); rec.Code != http.StatusAccepted {
		t.Errorf("after window = %d", rec.Code)
	}
	call(h, http.MethodPost, "Bearer gst_bad")
	if len(s.events) != 2 {
		t.Errorf("events = %d, want 2 (one per window)", len(s.events))
	}
}

func TestLimitIsPerAddress(t *testing.T) {
	h, _, _ := newHandler()
	for range 3 {
		call(h, http.MethodPost, "Bearer gst_bad")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/trigger", nil)
	req.RemoteAddr = "198.51.100.9:1"
	req.Header.Set("Authorization", "Bearer gst_good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("other address blocked: %d", rec.Code)
	}
}

func TestCustomClientIP(t *testing.T) {
	h, _, _ := newHandler()
	h.ClientIP = func(r *http.Request) string { return r.Header.Get("X-Forwarded-For") }
	for range 3 {
		req := httptest.NewRequest(http.MethodPost, "/api/trigger", nil)
		req.Header.Set("X-Forwarded-For", "192.0.2.1")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if !h.blocked("192.0.2.1") {
		t.Error("forwarded address not counted")
	}
}
