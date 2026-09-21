package strava

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// mock is a tiny stand-in for Strava's edit page.
type mock struct {
	mu          sync.Mutex
	description string
	posts       int
	ignorePosts bool // simulate a save the server silently drops
	noTextarea  bool
	rotate      bool // send a new session cookie on GET
}

func (m *mock) handler() http.Handler {
	mux := http.NewServeMux()
	loggedIn := func(r *http.Request) bool {
		c, err := r.Cookie("_strava4_session")
		return err == nil && c.Value == "ok"
	}
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<h1>Log in</h1>")
	})
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		if !loggedIn(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fmt.Fprint(w, "<h1>Dashboard</h1>")
	})
	mux.HandleFunc("/athlete/training_activities", func(w http.ResponseWriter, r *http.Request) {
		if !loggedIn(r) {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			http.Error(w, "need ajax header", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"models":[{"id":42,"name":"Run","type":"Run","start_time":"2026-09-20T05:00:00Z","elapsed_time":1200}]}`)
	})
	mux.HandleFunc("/activities/42/edit", func(w http.ResponseWriter, r *http.Request) {
		if !loggedIn(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.rotate {
			http.SetCookie(w, &http.Cookie{Name: "_strava4_session", Value: "ok", Path: "/", MaxAge: 3600})
			http.SetCookie(w, &http.Cookie{Name: "rotated", Value: "yes", Path: "/", MaxAge: 3600})
		}
		field := `<textarea name="activity[description]" id="activity_description">` + html.EscapeString(m.description) + `</textarea>`
		if m.noTextarea {
			field = `<p>Something else</p>`
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><body><form method="post" action="/activities/42"><input name="x" value="1">%s
			<button type="submit">Save</button></form></body></html>`, field)
	})
	mux.HandleFunc("/activities/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			m.mu.Lock()
			m.posts++
			if !m.ignorePosts {
				m.description = r.Form.Get("activity[description]")
			}
			m.mu.Unlock()
		}
		fmt.Fprint(w, "<h1>Activity</h1>")
	})
	return mux
}

func chromeOrSkip(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	for _, n := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	t.Skip("no Chrome/Chromium found; set CHROME_PATH to run browser tests")
	return ""
}

func newWriter(t *testing.T, m *mock, cookies []Cookie, save func([]Cookie) error) *Writer {
	t.Helper()
	path := chromeOrSkip(t)
	srv := httptest.NewServer(m.handler())
	t.Cleanup(srv.Close)
	return NewWriter(Config{
		BaseURL:       srv.URL,
		ChromePath:    path,
		NoSandbox:     true,
		LoadCookies:   func() ([]Cookie, error) { return cookies, nil },
		SaveCookies:   save,
		LocateTimeout: 2 * time.Second,
		SaveTimeout:   5 * time.Second,
		Timeout:       60 * time.Second,
	})
}

var goodCookies = []Cookie{{Name: "_strava4_session", Value: "ok"}}

func TestUpdateDescription(t *testing.T) {
	m := &mock{description: "My run"}
	w := newWriter(t, m, goodCookies, nil)

	err := w.UpdateDescription(context.Background(), "42", func(existing string) string {
		return existing + "\n\n🩸 TIR 92% | min 78 | max 164 | avg 112 mg/dL\n▁▂▃"
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "My run\n\n🩸 TIR 92% | min 78 | max 164 | avg 112 mg/dL\n▁▂▃"; strings.ReplaceAll(m.description, "\r\n", "\n") != want {
		t.Errorf("description = %q", m.description)
	}
	if m.posts != 1 {
		t.Errorf("posts = %d, want 1", m.posts)
	}

	// Same result again: nothing to save.
	err = w.UpdateDescription(context.Background(), "42", func(existing string) string { return existing })
	if err != nil || m.posts != 1 {
		t.Errorf("no-op run: err=%v posts=%d", err, m.posts)
	}
}

func TestSessionExpired(t *testing.T) {
	w := newWriter(t, &mock{}, []Cookie{{Name: "_strava4_session", Value: "stale"}}, nil)
	err := w.UpdateDescription(context.Background(), "42", func(s string) string { return s + "x" })
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("err = %v, want ErrSessionExpired", err)
	}
	if err := w.CheckSession(context.Background()); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("CheckSession err = %v, want ErrSessionExpired", err)
	}
}

func TestNoCookiesIsSessionExpired(t *testing.T) {
	w := newWriter(t, &mock{}, nil, nil)
	err := w.UpdateDescription(context.Background(), "42", func(s string) string { return s })
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("err = %v", err)
	}
}

func TestCheckSessionOK(t *testing.T) {
	w := newWriter(t, &mock{}, goodCookies, nil)
	if err := w.CheckSession(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSelectorMissing(t *testing.T) {
	w := newWriter(t, &mock{noTextarea: true}, goodCookies, nil)
	err := w.UpdateDescription(context.Background(), "42", func(s string) string { return s + "x" })
	var se *SelectorError
	if !errors.As(err, &se) || se.Key != "description" || len(se.Tried) == 0 {
		t.Fatalf("err = %v, want SelectorError for description", err)
	}
}

func TestVerificationCatchesDroppedSave(t *testing.T) {
	m := &mock{description: "x", ignorePosts: true}
	w := newWriter(t, m, goodCookies, nil)
	err := w.UpdateDescription(context.Background(), "42", func(s string) string { return s + " more" })
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Errorf("err = %v, want verification failure", err)
	}
}

func TestCookiesSavedAfterRotation(t *testing.T) {
	var saved []Cookie
	m := &mock{description: "a", rotate: true}
	w := newWriter(t, m, goodCookies, func(c []Cookie) error { saved = c; return nil })

	if err := w.UpdateDescription(context.Background(), "42", func(s string) string { return s + "b" }); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, c := range saved {
		names[c.Name] = true
	}
	if !names["_strava4_session"] || !names["rotated"] {
		t.Errorf("saved cookies = %+v", saved)
	}
}

func TestInvalidID(t *testing.T) {
	w := NewWriter(Config{LoadCookies: func() ([]Cookie, error) { return goodCookies, nil }})
	for _, id := range []string{"", "abc", "42/../x", "42?x=1"} {
		if err := w.UpdateDescription(context.Background(), id, func(s string) string { return s }); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
}

func TestInspect(t *testing.T) {
	m := &mock{description: "Hello"}
	w := newWriter(t, m, goodCookies, nil)

	rep, err := w.Inspect(context.Background(), "42", true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.LoggedIn || rep.DescriptionSelector != `textarea[name="activity[description]"]` ||
		rep.Description != "Hello" || rep.SaveMethod != "form-button" {
		t.Errorf("report = %+v", rep)
	}
	if len(rep.Textareas) != 1 || rep.Textareas[0].Name != "activity[description]" || len(rep.Buttons) != 1 {
		t.Errorf("inventory = %+v / %+v", rep.Textareas, rep.Buttons)
	}
	if !strings.Contains(rep.HTML, "<textarea") || m.posts != 0 {
		t.Errorf("html=%d bytes posts=%d", len(rep.HTML), m.posts)
	}
	if !strings.Contains(rep.String(), "matched") {
		t.Errorf("String() = %s", rep)
	}
}

func TestInspectMissingSelectorStillReports(t *testing.T) {
	w := newWriter(t, &mock{noTextarea: true}, goodCookies, nil)
	rep, err := w.Inspect(context.Background(), "42", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DescriptionSelector != "" || !strings.Contains(rep.String(), "NOT FOUND") {
		t.Errorf("report = %s", rep)
	}
}

func TestInspectLoggedOut(t *testing.T) {
	w := newWriter(t, &mock{}, []Cookie{{Name: "_strava4_session", Value: "stale"}}, nil)
	rep, err := w.Inspect(context.Background(), "42", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.LoggedIn || !strings.Contains(rep.String(), "not valid") {
		t.Errorf("report = %s", rep)
	}
}

func TestListRecent(t *testing.T) {
	w := newWriter(t, &mock{}, goodCookies, nil)
	got, err := w.ListRecent(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].StravaID != "42" || got[0].Duration != 20*time.Minute {
		t.Errorf("got %+v", got)
	}
}

func TestListRecentSessionExpired(t *testing.T) {
	w := newWriter(t, &mock{}, []Cookie{{Name: "_strava4_session", Value: "stale"}}, nil)
	if _, err := w.ListRecent(context.Background(), 5); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("err = %v, want ErrSessionExpired", err)
	}
}
