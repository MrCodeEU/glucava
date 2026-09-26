package web

import (
	"net/http"
	"net/mail"
	"strings"

	"github.com/starfederation/datastar-go/datastar"
)

// MinPasswordLen is the shortest password the web UI account accepts.
const MinPasswordLen = 12

// actionAccount changes the signed-in user's email and/or password. It needs the
// current password, counts a wrong one like a failed login, and ends every other
// session; the browser that made the change gets a fresh cookie.
func (s *Server) actionAccount(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Current string `json:"accountCurrent"`
		Email   string `json:"accountEmail"`
		New     string `json:"accountNew"`
		Confirm string `json:"accountConfirm"`
	}
	readErr := datastar.ReadSignals(r, &v)
	rec, ok := s.userRecord(r)
	if !ok { // auth() already ran; only a token revoked in between gets here
		http.Error(w, "not signed in", http.StatusUnauthorized)
		return
	}
	fail := func(msg string) { s.toast(datastar.NewSSE(w, r), "error", msg) }
	if readErr != nil {
		fail("Could not read the form.")
		return
	}
	ip := s.Proxies.IP(r)
	if s.tooManyLogins(ip) {
		fail("Too many attempts. Wait a minute and try again.")
		return
	}
	if !rec.ValidatePassword(v.Current) {
		s.noteLoginFailure(ip)
		fail("The current password is wrong.")
		return
	}

	email := strings.TrimSpace(v.Email)
	if email != "" {
		a, err := mail.ParseAddress(email)
		if err != nil || a.Address != email {
			fail("That is not a valid email address.")
			return
		}
	}
	changeEmail := email != "" && email != rec.Email()
	changePass := v.New != ""
	switch {
	case changePass && len(v.New) < MinPasswordLen:
		fail("The new password must be at least 12 characters.")
		return
	case changePass && v.New != v.Confirm:
		fail("The new password and its confirmation differ.")
		return
	case !changeEmail && !changePass:
		fail("Nothing to change: enter a different email or a new password.")
		return
	}

	if changeEmail {
		rec.SetEmail(email)
	}
	if changePass {
		rec.SetPassword(v.New)
	}
	rec.RefreshTokenKey() // every other browser is signed out
	if err := s.App.Save(rec); err != nil {
		fail("Could not save: " + err.Error())
		return
	}
	if err := s.startSession(w, r, rec); err != nil {
		http.Error(w, "cannot create session", http.StatusInternalServerError)
		return
	}

	sse := datastar.NewSSE(w, r)
	_ = sse.PatchSignals([]byte(`{"accountCurrent":"","accountEmail":"","accountNew":"","accountConfirm":""}`))
	_ = sse.PatchElements(renderString(AccountEmail(rec.Email())))
	switch {
	case changeEmail && changePass:
		s.toast(sse, "ok", "Email and password changed. Other browsers are signed out.")
	case changeEmail:
		s.toast(sse, "ok", "Email changed. Other browsers are signed out.")
	default:
		s.toast(sse, "ok", "Password changed. Other browsers are signed out.")
	}
}
