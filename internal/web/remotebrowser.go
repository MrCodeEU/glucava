package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/strava"
)

// remoteTTL is how long a remote sign-in browser may stay open.
const remoteTTL = 8 * time.Minute

// remoteStart opens the remote browser. The session outlives this request;
// a goroutine stores the cookies when the user finishes signing in, whether
// or not the page is still open.
func (s *Server) remoteStart(w http.ResponseWriter, r *http.Request) {
	if s.RemoteStart == nil {
		http.Error(w, "not available", http.StatusNotFound)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.remote != nil {
		select {
		case <-s.remote.Done():
			s.remote = nil
		default:
			http.Error(w, "a sign-in browser is already open", http.StatusConflict)
			return
		}
	}
	sess, err := s.RemoteStart(r.Context(), remoteTTL)
	if err != nil {
		slog.Error("strava remote browser start failed", "err", err)
		code := http.StatusInternalServerError
		if errors.Is(err, strava.ErrRemoteBusy) {
			code = http.StatusConflict
		}
		// Chrome start errors carry no secrets (no cookies exist yet), and
		// the user needs the reason: a wrong CHROME_PATH looks the same as
		// a busy browser otherwise.
		http.Error(w, err.Error(), code)
		return
	}
	s.remote = sess
	slog.Info("strava remote sign-in browser opened")
	go s.remoteFinish(sess)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) remoteFinish(sess *strava.RemoteSession) {
	<-sess.Done()
	cookies, err := sess.Result()
	if err == nil {
		var enc string
		if enc, err = strava.EncodeCookies(cookies); err == nil {
			err = s.Vault.Set(secrets.NameStravaCookies, enc)
		}
	}
	s.mu.Lock()
	s.lastCheck.at, s.lastCheck.ok, s.lastCheck.err = time.Time{}, false, ""
	s.mu.Unlock()
	if err != nil {
		slog.Error("strava remote sign-in ended without cookies", "err", err)
		return
	}
	slog.Info("strava remote sign-in stored cookies", "count", len(cookies))
	s.Bus.Publish()
}

func (s *Server) remoteSession() *strava.RemoteSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remote
}

// remoteFrames streams JPEG frames as server-sent events: one base64 frame
// per "data:" line, then a final "done" event naming the outcome.
func (s *Server) remoteFrames(w http.ResponseWriter, r *http.Request) {
	sess := s.remoteSession()
	fl, ok := w.(http.Flusher)
	if sess == nil || !ok {
		http.Error(w, "no sign-in browser is open", http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	for {
		select {
		case <-r.Context().Done():
			return
		case f := <-sess.Frames():
			fmt.Fprintf(w, "data: %s\n\n", f)
			fl.Flush()
		case <-sess.Done():
			outcome := "ok"
			if _, err := sess.Result(); err != nil {
				outcome = "failed"
			}
			fmt.Fprintf(w, "event: done\ndata: %s\n\n", outcome)
			fl.Flush()
			return
		}
	}
}

func (s *Server) remoteInput(w http.ResponseWriter, r *http.Request) {
	sess := s.remoteSession()
	if sess == nil {
		http.Error(w, "no sign-in browser is open", http.StatusNotFound)
		return
	}
	var ev strava.RemoteInput
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "bad input", http.StatusBadRequest)
		return
	}
	if err := sess.Dispatch(ev); err != nil {
		http.Error(w, "browser closed", http.StatusGone)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) remoteStop(w http.ResponseWriter, r *http.Request) {
	if sess := s.remoteSession(); sess != nil {
		slog.Info("strava remote sign-in browser closed by user")
		sess.Close()
	}
	w.WriteHeader(http.StatusNoContent)
}
