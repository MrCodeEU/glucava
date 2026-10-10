package strava

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// A fake Strava with a login page whose only control is a big button at a
// known spot; clicking it signs in. The test drives it through Dispatch like
// the UI would.
func TestRemoteSessionSignIn(t *testing.T) {
	path := chromeOrSkip(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<body style="margin:0"><form method="post" action="/session">
<button style="position:absolute;left:100px;top:100px;width:300px;height:100px">Sign in</button></form></body>`)
	})
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "fresh", Path: "/"})
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<h1>Dashboard</h1>") })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	remoteLockWait = 200 * time.Millisecond
	t.Cleanup(func() { remoteLockWait = 30 * time.Second })
	w := NewWriter(Config{BaseURL: srv.URL, ChromePath: path, NoSandbox: true})
	s, err := w.StartRemote(context.Background(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := w.StartRemote(context.Background(), time.Minute); err != ErrRemoteBusy {
		t.Errorf("second session: err = %v, want ErrRemoteBusy", err)
	}
	select {
	case f := <-s.Frames():
		if len(f) == 0 {
			t.Error("empty frame")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no screencast frame")
	}
	for _, typ := range []string{"down", "up"} {
		if err := s.Dispatch(RemoteInput{Type: typ, X: 200, Y: 150, Button: "left"}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-s.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("session did not end after sign-in")
	}
	cookies, err := s.Result()
	if err != nil || !HasSession(cookies) {
		t.Errorf("cookies = %v, err = %v", cookies, err)
	}
}

func TestRemoteAllowedPages(t *testing.T) {
	s := &RemoteSession{w: NewWriter(Config{})}
	for host, want := range map[string]bool{
		"www.strava.com": true, "strava.com": true, "accounts.google.com": true, "appleid.apple.com": true,
		"www.facebook.com": true, "accounts.youtube.com": true, "evilyoutube.com": false, "accounts.google.at": true, "accounts.google.co.uk": true, "accounts.google.com.br": true, "google.evil.example": false, "evilgoogle.at": false, "evil.example": false, "strava.com.evil.example": false, "notgoogle.com": false,
	} {
		if got := s.allowedPage(host); got != want {
			t.Errorf("allowedPage(%q) = %v, want %v", host, got, want)
		}
	}
}

// Neither a top-level navigation nor a frame may reach a host outside the
// allowlists; here that host is a local name, which is also refused as private.
func TestRemoteNavigationLock(t *testing.T) {
	path := chromeOrSkip(t)
	var mu sync.Mutex
	hits := map[string]bool{}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path] = true
		mu.Unlock()
		fmt.Fprint(w, "other")
	}))
	defer other.Close()
	otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<body style="margin:0"><iframe src="%s/frame" width="10" height="10"></iframe>
<a href="%s/top" style="position:absolute;left:100px;top:100px;width:300px;height:100px;display:block">go</a></body>`, otherURL, otherURL)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := NewWriter(Config{BaseURL: srv.URL, ChromePath: path, NoSandbox: true})
	s, err := w.StartRemote(context.Background(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, typ := range []string{"down", "up"} {
		if err := s.Dispatch(RemoteInput{Type: typ, X: 200, Y: 150, Button: "left"}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(3 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	if hits["/frame"] {
		t.Error("frame from a disallowed host loaded")
	}
	if hits["/top"] {
		t.Error("top-level navigation to a disallowed host was not refused")
	}
}

func TestRemoteFrameAndPrivateHosts(t *testing.T) {
	s := &RemoteSession{w: NewWriter(Config{})}
	for host, want := range map[string]bool{
		"accounts.youtube.com": true, "accounts.google.at": true, "challenges.cloudflare.com": true,
		"evil.example": false, "169.254.169.254": false,
	} {
		if got := s.allowedFrame(host); got != want {
			t.Errorf("allowedFrame(%q) = %v, want %v", host, got, want)
		}
	}
	for host, want := range map[string]bool{
		"127.0.0.1": true, "10.0.0.5": true, "192.168.1.1": true, "169.254.169.254": true, "::1": true,
		"localhost": true, "nas.local": true, "www.strava.com": false, "8.8.8.8": false,
	} {
		if got := s.privateHost(host); got != want {
			t.Errorf("privateHost(%q) = %v, want %v", host, got, want)
		}
	}
}
