package strava

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	noFile      bool // the edit page has no photo input
	photo       []byte
	photoPosts  int
	rotate      bool // send a new session cookie on GET

	loginMode string // "", "challenge" or "wrong": how /login/submit behaves
}

func (m *mock) handler() http.Handler {
	mux := http.NewServeMux()
	loggedIn := func(r *http.Request) bool {
		c, err := r.Cookie("_strava4_session")
		return err == nil && c.Value == "ok"
	}
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<h1>Log in</h1><form method="post" action="/login/submit">
			<input name="email"><input name="password" type="password">
			<button id="login-button" type="submit">Log in</button></form>`)
	})
	mux.HandleFunc("/login/submit", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		mode := m.loginMode
		m.mu.Unlock()
		_ = r.ParseForm()
		switch mode {
		case "challenge":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<p>We sent a verification code to your phone. Enter the code to continue.</p>`)
		case "wrong":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<p>The email or password you entered is incorrect.</p>`)
		default:
			if r.Form.Get("email") == "" || r.Form.Get("password") == "" {
				http.Error(w, "missing credentials", http.StatusBadRequest)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "_strava4_session", Value: "ok", Path: "/"})
			http.Redirect(w, r, "/dashboard", http.StatusFound)
		}
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
		fileInput := `<input type="file" name="photo" accept="image/*">`
		if m.noFile {
			fileInput = ""
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><body><form method="post" action="/activities/42" enctype="multipart/form-data"><input name="x" value="1">%s%s
			<button type="submit">Save</button></form></body></html>`, field, fileInput)
	})
	mux.HandleFunc("/activities/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseMultipartForm(8 << 20)
			m.mu.Lock()
			m.posts++
			if f, _, err := r.FormFile("photo"); err == nil {
				m.photo, _ = io.ReadAll(f)
				m.photoPosts++
				_ = f.Close()
			}
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

func TestBrowserProfileIsRemoved(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	m := &mock{description: "My run"}
	w := newWriter(t, m, goodCookies, nil)
	if err := w.CheckSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(tmp, "glucava-chrome-*"))
	if len(left) != 0 {
		t.Errorf("browser profile left behind: %v", left)
	}
}

func TestLoginSuccess(t *testing.T) {
	m := &mock{}
	var saved []Cookie
	w := newWriter(t, m, nil, func(c []Cookie) error { saved = c; return nil })
	if err := w.Login(context.Background(), "me@example.test", "hunter2"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range saved {
		if c.Name == "_strava4_session" && c.Value == "ok" {
			found = true
		}
	}
	if !found {
		t.Errorf("session cookie not saved: %+v", saved)
	}
}

func TestLoginStopsOnChallenge(t *testing.T) {
	m := &mock{loginMode: "challenge"}
	w := newWriter(t, m, nil, func([]Cookie) error { t.Fatal("cookies saved on a blocked login"); return nil })
	err := w.Login(context.Background(), "me@example.test", "hunter2")
	var le *LoginError
	if !errors.As(err, &le) || le.Reason != BlockedChallenge {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginStopsOnWrongCredentials(t *testing.T) {
	m := &mock{loginMode: "wrong"}
	w := newWriter(t, m, nil, func([]Cookie) error { t.Fatal("cookies saved on a blocked login"); return nil })
	err := w.Login(context.Background(), "me@example.test", "wrong")
	var le *LoginError
	if !errors.As(err, &le) || le.Reason != BlockedCredentials {
		t.Fatalf("err = %v", err)
	}
}

// Chrome's own startup can take longer than chromedp's 20 second default on a
// busy CI runner or a small homelab host, which showed up as flaky
// "websocket url timeout reached" failures.
func TestStartTimeoutDefaultsAndOverrides(t *testing.T) {
	if got := NewWriter(Config{}).cfg.StartTimeout; got != 60*time.Second {
		t.Errorf("default StartTimeout = %v, want 60s", got)
	}
	if got := NewWriter(Config{StartTimeout: 5 * time.Second}).cfg.StartTimeout; got != 5*time.Second {
		t.Errorf("explicit StartTimeout = %v, want 5s", got)
	}
}

func TestUploadPhoto(t *testing.T) {
	m := &mock{description: "My run"}
	w := newWriter(t, m, goodCookies, nil)
	w.cfg.UploadWait = 100 * time.Millisecond

	png := []byte("\x89PNG\r\n\x1a\nfake")
	if err := w.UploadPhoto(context.Background(), "42", "glucose.png", png); err != nil {
		t.Fatal(err)
	}
	if string(m.photo) != string(png) || m.photoPosts != 1 {
		t.Errorf("server got %d bytes in %d posts, want %d", len(m.photo), m.photoPosts, len(png))
	}
	if m.description != "My run" {
		t.Errorf("description changed to %q", m.description)
	}
}

func TestUploadPhotoWithoutFileInput(t *testing.T) {
	w := newWriter(t, &mock{description: "x", noFile: true}, goodCookies, nil)
	w.cfg.UploadWait = 100 * time.Millisecond
	var se *SelectorError
	if err := w.UploadPhoto(context.Background(), "42", "glucose.png", []byte("x")); !errors.As(err, &se) || se.Key != "photo" {
		t.Errorf("err = %v, want a photo SelectorError", err)
	}
}

func TestUploadPhotoSessionExpired(t *testing.T) {
	w := newWriter(t, &mock{}, []Cookie{{Name: "_strava4_session", Value: "stale"}}, nil)
	if err := w.UploadPhoto(context.Background(), "42", "glucose.png", []byte("x")); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("err = %v", err)
	}
}
