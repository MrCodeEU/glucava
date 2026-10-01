package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

func getWithLang(e *env, path, acceptLanguage string, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptLanguage != "" {
		r.Header.Set("Accept-Language", acceptLanguage)
	}
	if c != nil {
		r.AddCookie(c)
	}
	return e.do(r)
}

func TestLoginFollowsAcceptLanguage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)

	en := getWithLang(e, "/login", "", nil).Body.String()
	if !strings.Contains(en, `<html lang="en"`) || !strings.Contains(en, i18n.English().T("login.submit")) {
		t.Errorf("default login page is not English: %.300s", en)
	}
	de := getWithLang(e, "/login", "de-AT,de;q=0.9,en;q=0.5", nil).Body.String()
	want := i18n.Default().For("de-AT").T("login.submit")
	if !strings.Contains(de, `<html lang="de-AT"`) || !strings.Contains(de, want) || want == i18n.English().T("login.submit") {
		t.Errorf("Accept-Language de-AT not honoured (want %q): %.300s", want, de)
	}
}

func TestLanguageSettingOverridesBrowser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	de := i18n.Default().For("de")

	// Default "auto": the browser decides.
	if body := getWithLang(e, "/events", "de", c).Body.String(); !strings.Contains(body, de.T("events.sub")) {
		t.Errorf("auto did not follow Accept-Language de")
	}

	// An explicit choice wins over the browser, and the page it reloads to
	// already uses it (the cached setting is dropped on save).
	save := strings.Replace(validSettings, `{"unit"`, `{"language":"en","unit"`, 1)
	if w := e.action("/actions/settings", save, c, map[string]string{"Accept-Language": "de"}); !strings.Contains(w.Body.String(), "Settings saved") {
		t.Fatalf("save = %s", w.Body)
	}
	cfg, _ := e.srv.Store.LoadConfig()
	if cfg.Language != "en" {
		t.Fatalf("stored language = %q", cfg.Language)
	}
	body := getWithLang(e, "/events", "de", c).Body.String()
	if strings.Contains(body, de.T("events.sub")) || !strings.Contains(body, i18n.English().T("events.sub")) || !strings.Contains(body, `<html lang="en"`) {
		t.Errorf("explicit en ignored with Accept-Language de")
	}

	// And the other way round: explicit de with an English browser.
	save = strings.Replace(validSettings, `{"unit"`, `{"language":"de","unit"`, 1)
	e.action("/actions/settings", save, c, nil)
	body = getWithLang(e, "/events", "en-US", c).Body.String()
	if !strings.Contains(body, de.T("events.sub")) || !strings.Contains(body, de.T("nav.notifications")) {
		t.Errorf("explicit de ignored with Accept-Language en")
	}
}

func TestLanguageSettingValidation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	save := strings.Replace(validSettings, `{"unit"`, `{"language":"xx-nope","unit"`, 1)
	w := e.action("/actions/settings", save, c, nil)
	if !strings.Contains(w.Body.String(), "Language must be") {
		t.Errorf("unknown language accepted: %s", w.Body)
	}
}

func TestSettingsPageListsInstalledLanguages(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	body := getWithLang(e, "/settings", "", c).Body.String()
	for _, l := range i18n.Default().Locales() {
		if !strings.Contains(body, `value="`+l.Tag+`"`) || !strings.Contains(body, l.NativeName) {
			t.Errorf("language %s missing from the Settings select", l.Tag)
		}
	}
	if !strings.Contains(body, `value="auto"`) {
		t.Error("Automatic option missing")
	}
}

// Datastar patches and toasts come from handlers that run under the same
// middleware, so s.tr(r) is the request's language there too.
func TestSSEHandlersSeeTheRequestLanguage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var got string
	h := e.srv.withTranslator(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = e.srv.tr(r).Tag() }))
	r := httptest.NewRequest(http.MethodGet, "/stream/live", nil)
	r.Header.Set("Accept-Language", "de")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got != "de" {
		t.Errorf("translator in a stream handler = %q", got)
	}
}
