package strava

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		"www.facebook.com": true, "evil.example": false, "strava.com.evil.example": false, "notgoogle.com": false,
	} {
		if got := s.allowedPage(host); got != want {
			t.Errorf("allowedPage(%q) = %v, want %v", host, got, want)
		}
	}
}
