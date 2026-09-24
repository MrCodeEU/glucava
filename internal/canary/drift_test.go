package canary

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/strava"
)

// driftPage serves a stand-in for Strava's edit page whose markup can be
// changed between checks, to prove the canary notices each kind of drift.
type driftPage struct {
	mu      sync.Mutex
	variant string
}

func (d *driftPage) set(v string) { d.mu.Lock(); d.variant = v; d.mu.Unlock() }

func (d *driftPage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	v := d.variant
	d.mu.Unlock()
	c, err := r.Cookie("_strava4_session")
	if v == "loggedout" || err != nil || c.Value != "ok" {
		if r.URL.Path == "/login" {
			fmt.Fprint(w, "<h1>Log in</h1>")
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	var body string
	switch v {
	case "ok":
		// Like the real page: description textarea has no name or id.
		body = `<form method="post" action="/activities/42"><textarea>run</textarea>
			<textarea id="activity_private_note"></textarea><button type="submit">Save</button></form>`
	case "german":
		body = `<form method="post" action="/activities/42"><textarea aria-label="Wie ist es gelaufen?">run</textarea>
			<textarea id="activity_private_note"></textarea><button type="submit">Speichern</button></form>`
	case "editor":
		body = `<form method="post" action="/activities/42"><div contenteditable="true" data-testid="desc">run</div>
			<button type="submit">Save</button></form>`
	case "nosave":
		body = `<textarea name="activity[description]">run</textarea><div class="btn">Save</div>`
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html><html><body>%s</body></html>", body)
}

func TestDriftIsDetected(t *testing.T) {
	chrome := os.Getenv("CHROME_PATH")
	if chrome == "" {
		for _, n := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
			if p, err := exec.LookPath(n); err == nil {
				chrome = p
				break
			}
		}
	}
	if chrome == "" {
		t.Skip("no Chrome/Chromium found; set CHROME_PATH to run browser tests")
	}

	page := &driftPage{variant: "ok"}
	srv := httptest.NewServer(page)
	t.Cleanup(srv.Close)

	newRunner := func(sel strava.Selectors) (*Runner, *fakeStore) {
		w := strava.NewWriter(strava.Config{
			BaseURL: srv.URL, ChromePath: chrome, NoSandbox: true,
			LoadCookies:   func() ([]strava.Cookie, error) { return []strava.Cookie{{Name: "_strava4_session", Value: "ok"}}, nil },
			Selectors:     sel,
			LocateTimeout: time.Second, SaveTimeout: 3 * time.Second, Timeout: time.Minute,
		})
		st := &fakeStore{acts: []jobs.Activity{{StravaID: "42"}}}
		return &Runner{Inspector: w, Store: st}, st
	}

	ctx := context.Background()
	tests := []struct {
		variant string
		sel     strava.Selectors
		wantErr bool
		wantMsg string
	}{
		{"ok", strava.Selectors{}, false, ""},
		{"german", strava.Selectors{}, false, ""}, // localized labels must not trip it
		{"editor", strava.Selectors{}, true, "description field was not found"},
		{"editor", strava.Selectors{Description: []string{`[data-testid="desc"]`}}, false, ""}, // override recovers
		{"nosave", strava.Selectors{}, true, "no way to save"},
		{"loggedout", strava.Selectors{}, true, "login page"},
	}
	for _, tc := range tests {
		t.Run(tc.variant, func(t *testing.T) {
			page.set(tc.variant)
			r, st := newRunner(tc.sel)
			err := r.Once(ctx)
			if !tc.wantErr {
				if err != nil || len(st.events) != 0 {
					t.Fatalf("err=%v events=%v, want a clean pass", err, st.events)
				}
				return
			}
			if err == nil || len(st.events) != 1 || st.events[0].Type != jobs.EventCanaryFailed {
				t.Fatalf("err=%v events=%v, want one canary_failed", err, st.events)
			}
			if got := st.events[0].Message; !strings.Contains(got, tc.wantMsg) {
				t.Errorf("message %q does not mention %q", got, tc.wantMsg)
			}
			// The same drift on the next run must not notify again.
			_ = r.Once(ctx)
			if len(st.events) != 1 {
				t.Errorf("repeat check produced %d events, want 1", len(st.events))
			}
			// Fixing the page re-arms the canary.
			page.set("ok")
			if err := r.Once(ctx); err != nil {
				t.Errorf("recovered page still fails: %v", err)
			}
		})
	}
}
