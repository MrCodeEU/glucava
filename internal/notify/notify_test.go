package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func TestNtfy(t *testing.T) {
	var got *http.Request
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}))
	defer srv.Close()

	n := &Ntfy{URL: srv.URL + "/topic", Token: "tk_secret"}
	err := n.Send(context.Background(), Message{Type: "strava_failed", Severity: "error", Title: "Strava update failed", Body: "activity 42: boom"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/topic" || body != "activity 42: boom" {
		t.Errorf("request = %s %s body=%q", got.Method, got.URL.Path, body)
	}
	if got.Header.Get("Title") != "Strava update failed" || got.Header.Get("Priority") != "4" ||
		got.Header.Get("Authorization") != "Bearer tk_secret" {
		t.Errorf("headers = %v", got.Header)
	}
}

func TestWebhookSigned(t *testing.T) {
	var body []byte
	var sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		sig = r.Header.Get("X-Glucava-Signature")
	}))
	defer srv.Close()

	w := &Webhook{URL: srv.URL, Secret: "s3cret"}
	if err := w.Send(context.Background(), Message{Type: "session_expired", Severity: "error", Body: "x", Time: t0}); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	if sig != hex.EncodeToString(mac.Sum(nil)) {
		t.Error("signature does not match body")
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil || m["type"] != "session_expired" || m["time"] != "2026-09-20T12:00:00Z" {
		t.Errorf("body = %s (%v)", body, err)
	}
}

func TestHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := (&Ntfy{URL: srv.URL}).Send(context.Background(), Message{}); err == nil {
		t.Error("expected error for HTTP 500")
	}
	if err := (&Webhook{URL: srv.URL}).Send(context.Background(), Message{}); err == nil {
		t.Error("expected error for HTTP 500")
	}
}

// --- dispatcher ---

type fakeOutbox struct {
	events   []OutboxEvent
	notified map[string]bool
}

func (o *fakeOutbox) Pending(context.Context) ([]OutboxEvent, error) {
	var out []OutboxEvent
	for _, e := range o.events {
		if !o.notified[e.ID] {
			out = append(out, e)
		}
	}
	return out, nil
}
func (o *fakeOutbox) MarkNotified(_ context.Context, id string) error {
	o.notified[id] = true
	return nil
}

type fakeChannel struct {
	fail bool
	sent []Message
}

func (c *fakeChannel) Name() string { return "fake" }
func (c *fakeChannel) Send(_ context.Context, m Message) error {
	if c.fail {
		return errors.New("down")
	}
	c.sent = append(c.sent, m)
	return nil
}

func newDispatcher(events []OutboxEvent, chans ...Channel) (*Dispatcher, *fakeOutbox, *time.Time) {
	now := t0.Add(time.Minute)
	ob := &fakeOutbox{events: events, notified: map[string]bool{}}
	return &Dispatcher{Outbox: ob, Channels: func() []Channel { return chans }, Now: func() time.Time { return now }}, ob, &now
}

func ev(id, typ, strava string) OutboxEvent {
	return OutboxEvent{ID: id, Type: typ, Severity: "error", Message: "m " + id, StravaID: strava, Created: t0}
}

func TestFlushDeliversAndMarks(t *testing.T) {
	ch := &fakeChannel{}
	d, ob, _ := newDispatcher([]OutboxEvent{ev("1", "strava_failed", "42")}, ch)
	if err := d.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ch.sent) != 1 || ch.sent[0].Title != "Strava update failed" || !ob.notified["1"] {
		t.Errorf("sent=%+v notified=%v", ch.sent, ob.notified)
	}
}

func TestCooldownSuppressesRepeats(t *testing.T) {
	ch := &fakeChannel{}
	d, ob, now := newDispatcher([]OutboxEvent{
		ev("1", "session_expired", ""), ev("2", "session_expired", ""), ev("3", "strava_failed", "7"),
	}, ch)
	if err := d.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ch.sent) != 2 {
		t.Fatalf("sent %d, want 2 (duplicate suppressed)", len(ch.sent))
	}
	if !ob.notified["2"] {
		t.Error("suppressed event must still be marked notified")
	}

	// After the cooldown a new one is sent again.
	*now = now.Add(7 * time.Hour)
	ob.events = append(ob.events, OutboxEvent{ID: "4", Type: "session_expired", Severity: "error", Created: *now})
	_ = d.Flush(context.Background())
	if len(ch.sent) != 3 {
		t.Errorf("sent %d, want 3 after cooldown", len(ch.sent))
	}
}

func TestAllChannelsFailKeepsPending(t *testing.T) {
	bad := &fakeChannel{fail: true}
	d, ob, _ := newDispatcher([]OutboxEvent{ev("1", "strava_failed", "1")}, bad)
	_ = d.Flush(context.Background())
	if ob.notified["1"] {
		t.Fatal("event marked notified although nothing was delivered")
	}
	bad.fail = false
	_ = d.Flush(context.Background())
	if !ob.notified["1"] || len(bad.sent) != 1 {
		t.Errorf("retry failed: notified=%v sent=%d", ob.notified, len(bad.sent))
	}
}

func TestOneChannelEnough(t *testing.T) {
	bad, good := &fakeChannel{fail: true}, &fakeChannel{}
	d, ob, _ := newDispatcher([]OutboxEvent{ev("1", "strava_failed", "1")}, bad, good)
	_ = d.Flush(context.Background())
	if !ob.notified["1"] || len(good.sent) != 1 {
		t.Errorf("notified=%v sent=%d", ob.notified, len(good.sent))
	}
}

func TestNoChannelsKeepsEvents(t *testing.T) {
	d, ob, _ := newDispatcher([]OutboxEvent{ev("1", "strava_failed", "1")})
	_ = d.Flush(context.Background())
	if ob.notified["1"] {
		t.Error("event dropped while no channel is configured")
	}
}

func TestOldEventsDropped(t *testing.T) {
	ch := &fakeChannel{}
	d, ob, now := newDispatcher([]OutboxEvent{ev("1", "strava_failed", "1")}, ch)
	*now = t0.Add(48 * time.Hour)
	_ = d.Flush(context.Background())
	if len(ch.sent) != 0 || !ob.notified["1"] {
		t.Errorf("sent=%d notified=%v", len(ch.sent), ob.notified)
	}
}

func TestNoRedirectsFollowed(t *testing.T) {
	hit := false
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()

	n := &Ntfy{URL: srv.URL, Token: "secret"}
	err := n.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil || hit {
		t.Errorf("err = %v, redirect target hit = %v", err, hit)
	}
}

func TestDispatcherAddsLink(t *testing.T) {
	ch := &fakeChannel{}
	d, _, _ := newDispatcher([]OutboxEvent{ev("1", "strava_failed", "42")}, ch)
	d.Link = func(m Message) (string, string) { return LinkFor("https://g.example/", m) }
	if err := d.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ch.sent[0]; got.Link != "https://g.example/activity/42" || got.LinkLabel != "Open in glucava" {
		t.Errorf("link = %q / %q", got.Link, got.LinkLabel)
	}
}
