package web

import (
	"log"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/MrCodeEU/glucava/internal/migrations"
)

const (
	maxLoginFailures = 10
	loginWindow      = time.Minute
)

type loginBucket struct {
	start time.Time
	count int
}

// user returns the signed-in user's email from the auth cookie.
func (s *Server) user(r *http.Request) (string, bool) {
	c, err := r.Cookie(authCookie)
	if err != nil || c.Value == "" {
		return "", false
	}
	rec, err := s.App.FindAuthRecordByToken(c.Value, core.TokenTypeAuth)
	if err != nil || rec.Collection().Name != "users" {
		return "", false
	}
	return rec.Email(), true
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.user(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.html(w, http.StatusOK, LoginPage(s.Build, "", ""))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	ip := s.Proxies.IP(r)
	if s.tooManyLogins(ip) {
		w.Header().Set("Retry-After", "60")
		s.html(w, http.StatusTooManyRequests, LoginPage(s.Build, "Too many attempts. Wait a minute and try again.", r.PostFormValue("email")))
		return
	}

	email := r.PostFormValue("email")
	rec, err := s.App.FindAuthRecordByEmail("users", email)
	if err != nil || !rec.ValidatePassword(r.PostFormValue("password")) {
		s.noteLoginFailure(ip)
		s.html(w, http.StatusUnauthorized, LoginPage(s.Build, "Wrong email or password.", email))
		return
	}
	token, err := rec.NewAuthToken()
	if err != nil {
		http.Error(w, "cannot create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: authCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.Proxies.Secure(r), MaxAge: migrations.SessionSeconds,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	// Rotating the token key makes the old token invalid everywhere, not just in this browser.
	if c, err := r.Cookie(authCookie); err == nil {
		if rec, err := s.App.FindAuthRecordByToken(c.Value, core.TokenTypeAuth); err == nil {
			rec.RefreshTokenKey()
			if err := s.App.Save(rec); err != nil {
				log.Printf("web: revoke session: %v", err)
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: authCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Proxies.Secure(r), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) tooManyLogins(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.failures[ip]
	return b != nil && s.now().Sub(b.start) < loginWindow && b.count >= maxLoginFailures
}

func (s *Server) noteLoginFailure(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures == nil {
		s.failures = map[string]*loginBucket{}
	}
	b := s.failures[ip]
	if b == nil || s.now().Sub(b.start) >= loginWindow {
		b = &loginBucket{start: s.now()}
		s.failures[ip] = b
	}
	b.count++
}
