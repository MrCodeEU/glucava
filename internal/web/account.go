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
		http.Error(w, s.tr(r).T("err.account.not_signed_in"), http.StatusUnauthorized)
		return
	}
	fail := func(msg string) { s.toast(datastar.NewSSE(w, r), "error", msg) }
	if readErr != nil {
		fail(s.tr(r).T("err.form"))
		return
	}
	ip := s.Proxies.IP(r)
	if s.tooManyLogins(ip) {
		fail(s.tr(r).T("login.error.too_many"))
		return
	}
	if !rec.ValidatePassword(v.Current) {
		s.noteLoginFailure(ip)
		fail(s.tr(r).T("err.account.wrong_password"))
		return
	}

	email := strings.TrimSpace(v.Email)
	if email != "" {
		a, err := mail.ParseAddress(email)
		if err != nil || a.Address != email {
			fail(s.tr(r).T("err.account.bad_email"))
			return
		}
	}
	changeEmail := email != "" && email != rec.Email()
	changePass := v.New != ""
	switch {
	case changePass && len(v.New) < MinPasswordLen:
		fail(s.tr(r).T("err.account.password_short", "n", MinPasswordLen))
		return
	case changePass && v.New != v.Confirm:
		fail(s.tr(r).T("err.account.password_mismatch"))
		return
	case !changeEmail && !changePass:
		fail(s.tr(r).T("err.account.nothing"))
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
		fail(s.tr(r).T("err.save", "error", err.Error()))
		return
	}
	if err := s.startSession(w, r, rec); err != nil {
		http.Error(w, s.tr(r).T("err.http.session"), http.StatusInternalServerError)
		return
	}

	sse := datastar.NewSSE(w, r)
	_ = sse.PatchSignals([]byte(`{"accountCurrent":"","accountEmail":"","accountNew":"","accountConfirm":""}`))
	_ = sse.PatchElements(renderString(AccountEmailT(s.tr(r), rec.Email())))
	switch {
	case changeEmail && changePass:
		s.toast(sse, "ok", s.tr(r).T("toast.account.both"))
	case changeEmail:
		s.toast(sse, "ok", s.tr(r).T("toast.account.email"))
	default:
		s.toast(sse, "ok", s.tr(r).T("toast.account.password"))
	}
}
