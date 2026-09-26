package web

import (
	"net/http"
	"strings"
	"testing"
)

func account(current, email, pass, confirm string) string {
	return `{"accountCurrent":"` + current + `","accountEmail":"` + email + `","accountNew":"` + pass + `","accountConfirm":"` + confirm + `"}`
}

func TestAccountChangeEmailAndPassword(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	w := e.action("/actions/account", account(testPass, "new@example.test", "a-brand-new-password", "a-brand-new-password"), c, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Email and password changed") {
		t.Fatalf("change = %d: %s", w.Code, w.Body)
	}
	var fresh *http.Cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == authCookie {
			fresh = ck
		}
	}
	if fresh == nil || fresh.Value == c.Value {
		t.Fatal("the browser that made the change should get a new session cookie")
	}
	if w := e.get(t, "/settings", c); w.Code != http.StatusSeeOther {
		t.Errorf("old session still works: %d", w.Code)
	}
	if w := e.get(t, "/settings", fresh); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "new@example.test") {
		t.Errorf("new session = %d", w.Code)
	}
	rec, err := e.app.FindAuthRecordByEmail("users", "new@example.test")
	if err != nil || !rec.ValidatePassword("a-brand-new-password") {
		t.Errorf("stored account was not updated: %v", err)
	}
}

func TestAccountRejectsBadInput(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	for name, tc := range map[string]struct{ body, want string }{
		"wrong current":   {account("nope", "x@example.test", "", ""), "current password is wrong"},
		"bad email":       {account(testPass, "not-an-email", "", ""), "not a valid email"},
		"short password":  {account(testPass, "", "short", "short"), "at least 12"},
		"mismatch":        {account(testPass, "", "long-enough-password", "different-password!"), "differ"},
		"nothing changed": {account(testPass, testEmail, "", ""), "Nothing to change"},
	} {
		w := e.action("/actions/account", tc.body, c, nil)
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("%s: %s", name, w.Body)
		}
		for _, ck := range w.Result().Cookies() {
			if ck.Name == authCookie {
				t.Errorf("%s: a rejected change must not touch the session", name)
			}
		}
	}
	if rec, err := e.app.FindAuthRecordByEmail("users", testEmail); err != nil || !rec.ValidatePassword(testPass) {
		t.Errorf("account changed despite every request being rejected: %v", err)
	}
	if w := e.get(t, "/settings", c); w.Code != http.StatusOK {
		t.Errorf("session ended by rejected changes: %d", w.Code)
	}
}

func TestAccountWrongPasswordIsRateLimited(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	for range maxLoginFailures {
		e.action("/actions/account", account("nope", "x@example.test", "", ""), c, nil)
	}
	w := e.action("/actions/account", account(testPass, "x@example.test", "", ""), c, nil)
	if !strings.Contains(w.Body.String(), "Too many attempts") {
		t.Errorf("no rate limit: %s", w.Body)
	}
}

func TestAccountNeedsLogin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if w := e.action("/actions/account", account(testPass, "x@example.test", "", ""), nil, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", w.Code)
	}
}
