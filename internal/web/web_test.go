package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	_ "github.com/pocketbase/pocketbase/migrations" // registers the system migrations

	"github.com/MrCodeEU/glucava/internal/bus"
	"github.com/MrCodeEU/glucava/internal/clientip"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/migrations"
	_ "github.com/MrCodeEU/glucava/internal/migrations"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/store"
	"github.com/MrCodeEU/glucava/internal/strava"
	"github.com/MrCodeEU/glucava/internal/tokens"
	"github.com/MrCodeEU/glucava/internal/trigger"
)

const (
	testEmail = "me@example.test"
	testPass  = "correct-horse-battery"
	origin    = "http://example.test"
)

type fakeJobs struct {
	mu   sync.Mutex
	got  []jobs.Job
	busy bool
}

func (f *fakeJobs) Enqueue(j jobs.Job) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, j)
	return !f.busy, nil
}

type fakeSession struct{ err error }

func (f fakeSession) CheckSession(context.Context) error { return f.err }

type env struct {
	srv  *Server
	h    http.Handler
	app  core.App
	jobs *fakeJobs
}

func newEnv(t *testing.T) *env {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })

	users, _ := app.FindCollectionByNameOrId("users")
	u := core.NewRecord(users)
	u.SetEmail(testEmail)
	u.SetPassword(testPass)
	u.SetVerified(true)
	if err := app.Save(u); err != nil {
		t.Fatal(err)
	}

	cipher, err := secrets.NewCipher([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	b := &bus.Bus{}
	fj := &fakeJobs{}
	srv := &Server{
		App: app, Store: &store.PB{App: app, Changed: b.Publish}, Vault: &secrets.Vault{App: app, Cipher: cipher},
		Tokens: &tokens.Manager{App: app}, Jobs: fj, Signal: trigger.NewSignal(), Bus: b,
		Session: fakeSession{}, SourceName: "dexcom", Build: "test", Loc: time.UTC,
	}
	return &env{srv: srv, h: srv.Handler(), app: app, jobs: fj}
}

func (e *env) do(r *http.Request) *httptest.ResponseRecorder {
	if r.Host == "" || r.Host == "example.com" {
		r.Host = "example.test"
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

// login returns the auth cookie for the test user.
func (e *env) login(t *testing.T) *http.Cookie {
	t.Helper()
	form := url.Values{"email": {testEmail}, "password": {testPass}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", origin)
	w := e.do(r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login = %d: %s", w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == authCookie {
			return c
		}
	}
	t.Fatal("no auth cookie")
	return nil
}

func (e *env) get(t *testing.T, path string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if c != nil {
		r.AddCookie(c)
	}
	return e.do(r)
}

// action posts JSON signals the way Datastar does.
func (e *env) action(path, body string, c *http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Datastar-Request", "true")
	r.Header.Set("Origin", origin)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	if c != nil {
		r.AddCookie(c)
	}
	return e.do(r)
}

func TestUnauthenticatedIsRedirectedOrRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, p := range []string{"/", "/strava", "/settings", "/tokens", "/events", "/activity/1"} {
		w := e.get(t, p, nil)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("GET %s = %d %q", p, w.Code, w.Header().Get("Location"))
		}
	}
	for _, p := range []string{"/actions/poll", "/actions/settings", "/actions/tokens/create", "/actions/strava/cookies", "/actions/reprocess/1"} {
		if w := e.action(p, "{}", nil, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("POST %s = %d, want 401", p, w.Code)
		}
	}
	if w := e.get(t, "/static/app.css", nil); w.Code != http.StatusOK {
		t.Errorf("static css = %d", w.Code)
	}
	if w := e.get(t, "/login", nil); w.Code != http.StatusOK {
		t.Errorf("login page = %d", w.Code)
	}
}

func TestLogin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure {
		t.Errorf("cookie = %+v", c)
	}
	for _, p := range []string{"/", "/strava", "/settings", "/tokens", "/events"} {
		if w := e.get(t, p, c); w.Code != http.StatusOK {
			t.Errorf("GET %s = %d", p, w.Code)
		}
	}
	if w := e.get(t, "/activity/nope", c); w.Code != http.StatusNotFound {
		t.Errorf("unknown activity = %d", w.Code)
	}

	// Behind an HTTPS proxy the cookie is Secure.
	form := url.Values{"email": {testEmail}, "password": {testPass}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", origin)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "10.0.0.1:1"
	for _, ck := range e.do(r).Result().Cookies() {
		if ck.Name == authCookie && ck.Secure {
			t.Error("header from an untrusted peer made the cookie Secure")
		}
	}
	e.srv.Proxies, _ = clientip.Parse("10.0.0.1")
	sawCookie := false
	for _, ck := range e.do(r).Result().Cookies() {
		if ck.Name == authCookie {
			sawCookie = true
			if !ck.Secure {
				t.Error("cookie not Secure behind a trusted https proxy")
			}
		}
	}
	if !sawCookie {
		t.Error("no auth cookie set")
	}
}

func TestLoginFailureGivesNoHint(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	post := func(email, pass string) *httptest.ResponseRecorder {
		form := url.Values{"email": {email}, "password": {pass}}
		r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		return e.do(r)
	}
	// Same typed email in both cases: only whether the account exists differs,
	// which must not be observable beyond the email echoed back verbatim.
	a, b := post(testEmail, "wrong"), post(testEmail, "also-wrong")
	if a.Code != http.StatusUnauthorized || b.Code != http.StatusUnauthorized {
		t.Fatalf("codes = %d, %d", a.Code, b.Code)
	}
	if a.Body.String() != b.Body.String() {
		t.Error("two wrong passwords for the same email gave different responses")
	}
	c := post("nobody@example.test", "wrong")
	if strings.ReplaceAll(a.Body.String(), testEmail, "X") != strings.ReplaceAll(c.Body.String(), "nobody@example.test", "X") {
		t.Error("unknown email and wrong password give different responses beyond the echoed email")
	}
	if len(a.Result().Cookies()) != 0 {
		t.Error("cookie set on failed login")
	}
}

func TestLoginRateLimit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	post := func() int {
		form := url.Values{"email": {testEmail}, "password": {"wrong"}}
		r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		r.RemoteAddr = "203.0.113.9:5555"
		return e.do(r).Code
	}
	for range maxLoginFailures {
		post()
	}
	if code := post(); code != http.StatusTooManyRequests {
		t.Errorf("after %d failures = %d, want 429", maxLoginFailures, code)
	}
}

func TestCrossSiteRequestsRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)

	if w := e.action("/actions/poll", "{}", c, map[string]string{"Origin": "http://evil.test"}); w.Code != http.StatusForbidden {
		t.Errorf("foreign Origin = %d, want 403", w.Code)
	}
	if w := e.action("/actions/poll", "{}", c, map[string]string{"Origin": "", "Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusForbidden {
		t.Errorf("cross-site fetch = %d, want 403", w.Code)
	}
	if w := e.action("/actions/poll", "{}", c, nil); w.Code != http.StatusOK {
		t.Errorf("same origin = %d, want 200", w.Code)
	}

	// Login and logout are protected too.
	form := url.Values{"email": {testEmail}, "password": {testPass}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://evil.test")
	if w := e.do(r); w.Code != http.StatusForbidden {
		t.Errorf("cross-site login = %d, want 403", w.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "/logout", nil)
	r.Header.Set("Origin", "http://evil.test")
	if w := e.do(r); w.Code != http.StatusForbidden {
		t.Errorf("cross-site logout = %d, want 403", w.Code)
	}
}

func TestSuperuserTokenIsNotAUserSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	col, err := e.app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	su := core.NewRecord(col)
	su.SetEmail("root@example.test")
	su.SetPassword("another-long-password")
	if err := e.app.Save(su); err != nil {
		t.Fatal(err)
	}
	tok, err := su.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	w := e.get(t, "/", &http.Cookie{Name: authCookie, Value: tok})
	if w.Code != http.StatusSeeOther {
		t.Errorf("superuser token accepted: %d", w.Code)
	}
	if w := e.get(t, "/", &http.Cookie{Name: authCookie, Value: "garbage"}); w.Code != http.StatusSeeOther {
		t.Errorf("garbage token accepted: %d", w.Code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	r := httptest.NewRequest(http.MethodPost, "/logout", nil)
	r.Header.Set("Origin", origin)
	r.AddCookie(c)
	w := e.do(r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("logout = %d", w.Code)
	}
	for _, ck := range w.Result().Cookies() {
		if ck.Name == authCookie && ck.MaxAge >= 0 {
			t.Errorf("cookie not cleared: %+v", ck)
		}
	}
}

const validSettings = `{"unit":"mmol/L","rangeLow":72,"rangeHigh":170,"preMin":15,"postMin":20,"pollMin":5,"lang":"de",
"dexcomRegion":"us","dexcomUsername":"me","dexcomPassword":"s3cret-dexcom","ntfyURL":"https://ntfy.example/t","ntfyToken":"tk-secret",
"webhookURL":"","webhookSecret":"","emailTo":"me@example.com"}`

func TestSettingsSaveAndSecretsStayOutOfHTML(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)

	w := e.action("/actions/settings", validSettings, c, nil)
	if !strings.Contains(w.Body.String(), "Settings saved") {
		t.Fatalf("response = %s", w.Body)
	}
	cfg, _ := e.srv.Store.LoadConfig()
	if cfg.Unit != "mmol/L" || cfg.RangeLow != 72 || cfg.PollMin != 5 || cfg.Lang != "de" || cfg.DexcomRegion != "us" || cfg.NtfyURL != "https://ntfy.example/t" || cfg.EmailTo != "me@example.com" {
		t.Errorf("config = %+v", cfg)
	}
	if v, ok, _ := e.srv.Vault.Get(secrets.NameDexcomPassword); !ok || v != "s3cret-dexcom" {
		t.Errorf("dexcom password = %q, %v", v, ok)
	}
	// Stored encrypted.
	rec, _ := e.app.FindFirstRecordByData("secrets", "name", secrets.NameDexcomPassword)
	if strings.Contains(rec.GetString("ciphertext"), "s3cret") {
		t.Error("secret stored in plaintext")
	}

	// The secrets never come back in any page.
	for _, p := range []string{"/settings", "/", "/strava", "/tokens", "/events"} {
		body := e.get(t, p, c).Body.String()
		if strings.Contains(body, "s3cret-dexcom") || strings.Contains(body, "tk-secret") {
			t.Errorf("%s leaks a secret", p)
		}
	}
	if !strings.Contains(e.get(t, "/settings", c).Body.String(), "A password is stored") {
		t.Error("settings page does not say the password is stored")
	}

	// An empty secret field keeps the stored value.
	e.action("/actions/settings", strings.Replace(validSettings, `"dexcomPassword":"s3cret-dexcom"`, `"dexcomPassword":""`, 1), c, nil)
	if v, _, _ := e.srv.Vault.Get(secrets.NameDexcomPassword); v != "s3cret-dexcom" {
		t.Errorf("empty field overwrote the password: %q", v)
	}
}

func TestSettingsValidation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	before, _ := e.srv.Store.LoadConfig()

	bad := map[string]string{
		"unit":         strings.Replace(validSettings, `"unit":"mmol/L"`, `"unit":"parsec"`, 1),
		"range order":  strings.Replace(validSettings, `"rangeHigh":170`, `"rangeHigh":60`, 1),
		"poll zero":    strings.Replace(validSettings, `"pollMin":5`, `"pollMin":0`, 1),
		"pre huge":     strings.Replace(validSettings, `"preMin":15`, `"preMin":9999`, 1),
		"region":       strings.Replace(validSettings, `"dexcomRegion":"us"`, `"dexcomRegion":"mars"`, 1),
		"ntfy scheme":  strings.Replace(validSettings, `https://ntfy.example/t`, `javascript:alert(1)`, 1),
		"webhook file": strings.Replace(validSettings, `"webhookURL":""`, `"webhookURL":"file:///etc/passwd"`, 1),
		"email":        strings.Replace(validSettings, `"emailTo":"me@example.com"`, `"emailTo":"not-an-address"`, 1),
		"not json":     `{"unit":`,
	}
	for name, body := range bad {
		w := e.action("/actions/settings", body, c, nil)
		if !strings.Contains(w.Body.String(), `data-variant="error"`) {
			t.Errorf("%s: not rejected: %s", name, w.Body)
		}
	}
	after, _ := e.srv.Store.LoadConfig()
	if after != before {
		t.Errorf("config changed by rejected input: %+v", after)
	}
}

func TestTokenFlow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)

	w := e.action("/actions/tokens/create", `{"tokenName":"phone"}`, c, nil)
	body := w.Body.String()
	i := strings.Index(body, "gst_")
	if i < 0 {
		t.Fatalf("no token in response: %s", body)
	}
	tok := body[i : i+47]
	if ok, _ := e.srv.Tokens.Verify(tok); !ok {
		t.Error("created token does not verify")
	}
	// The token is shown once: pages list names only.
	if strings.Contains(e.get(t, "/tokens", c).Body.String(), tok) {
		t.Error("tokens page shows the token again")
	}

	if w := e.action("/actions/tokens/create", `{"tokenName":"phone"}`, c, nil); !strings.Contains(w.Body.String(), "already exists") {
		t.Errorf("duplicate name not refused: %s", w.Body)
	}
	e.action("/actions/tokens/revoke/phone", "{}", c, nil)
	if ok, _ := e.srv.Tokens.Verify(tok); ok {
		t.Error("revoked token still verifies")
	}
}

func TestStravaCookieImport(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)

	w := e.action("/actions/strava/cookies", `{"cookies":"Cookie: _strava4_session=SUPERSECRETVALUE; sp=xyz"}`, c, nil)
	if !strings.Contains(w.Body.String(), "Stored 2 cookies") {
		t.Fatalf("response = %s", w.Body)
	}
	if strings.Contains(w.Body.String(), "SUPERSECRETVALUE") {
		t.Error("response echoes a cookie value")
	}
	page := e.get(t, "/strava", c).Body.String()
	if strings.Contains(page, "SUPERSECRETVALUE") || !strings.Contains(page, "_strava4_session") {
		t.Error("strava page must list cookie names only")
	}
	raw, ok, _ := e.srv.Vault.Get(secrets.NameStravaCookies)
	list, err := strava.DecodeCookies(raw)
	if !ok || err != nil || len(list) != 2 {
		t.Errorf("stored = %q, %v, %v", raw, ok, err)
	}

	if w := e.action("/actions/strava/cookies", `{"cookies":"   "}`, c, nil); !strings.Contains(w.Body.String(), `data-variant="error"`) {
		t.Errorf("empty import accepted: %s", w.Body)
	}
}

func TestSessionTest(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	e.action("/actions/strava/cookies", `{"cookies":"_strava4_session=x"}`, c, nil)

	if w := e.action("/actions/strava/test", "{}", c, nil); !strings.Contains(w.Body.String(), "accepts the stored cookies") {
		t.Errorf("ok case: %s", w.Body)
	}
	e.srv.Session = fakeSession{err: jobs.ErrSessionExpired}
	w := e.action("/actions/strava/test", "{}", c, nil)
	if !strings.Contains(w.Body.String(), "session test failed") || !strings.Contains(w.Body.String(), "no longer valid") {
		t.Errorf("expired case: %s", w.Body)
	}
}

func TestPollAndReprocess(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)

	e.action("/actions/poll", "{}", c, nil)
	select {
	case <-e.srv.Signal.C():
	default:
		t.Error("poll action did not kick the poller")
	}

	ctx := context.Background()
	_ = e.srv.Store.SaveActivity(ctx, &jobs.Activity{StravaID: "77", Name: "Run", Start: time.Now().Add(-3 * time.Hour), Duration: time.Hour, Status: jobs.StatusFailed, Error: "boom"})
	e.action("/actions/reprocess/77", "{}", c, nil)
	if len(e.jobs.got) != 1 || !e.jobs.got[0].Force || e.jobs.got[0].Activity.StravaID != "77" {
		t.Fatalf("jobs = %+v", e.jobs.got)
	}
	if a, _ := e.srv.Store.Activity(ctx, "77"); a.Status != jobs.StatusPending || a.Error != "" {
		t.Errorf("activity = %+v", a)
	}
	if w := e.action("/actions/reprocess/999", "{}", c, nil); !strings.Contains(w.Body.String(), "no longer exists") {
		t.Errorf("unknown id: %s", w.Body)
	}
}

func TestProcessActivityUnavailableWithoutFindActivity(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	if w := e.action("/actions/process", `{"processActivityId":"55"}`, c, nil); !strings.Contains(w.Body.String(), "not available") {
		t.Errorf("no FindActivity: %s", w.Body)
	}
}

func TestProcessActivityRejectsNonNumericID(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	e.srv.FindActivity = func(context.Context, string) (*jobs.Activity, error) {
		t.Fatal("FindActivity should not be called for a bad id")
		return nil, nil
	}
	if w := e.action("/actions/process", `{"processActivityId":"not-a-number"}`, c, nil); !strings.Contains(w.Body.String(), "numeric") {
		t.Errorf("bad id: %s", w.Body)
	}
}

func TestProcessActivityLooksUpAndQueuesUnknownID(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	start := time.Now().Add(-48 * time.Hour)
	e.srv.FindActivity = func(_ context.Context, id string) (*jobs.Activity, error) {
		if id != "42" {
			t.Fatalf("FindActivity called with %q", id)
		}
		return &jobs.Activity{StravaID: "42", Name: "Old run", Start: start, Duration: time.Hour, Status: jobs.StatusPending}, nil
	}
	w := e.action("/actions/process", `{"processActivityId":"42"}`, c, nil)
	if !strings.Contains(w.Body.String(), "Queued") {
		t.Errorf("body = %s", w.Body)
	}
	if len(e.jobs.got) != 1 || !e.jobs.got[0].Force || e.jobs.got[0].Activity.StravaID != "42" {
		t.Fatalf("jobs = %+v", e.jobs.got)
	}
	ctx := context.Background()
	if a, _ := e.srv.Store.Activity(ctx, "42"); a == nil || a.Status != jobs.StatusPending {
		t.Errorf("activity not saved: %+v", a)
	}
}

func TestProcessActivityReusesExistingRowWithoutLookup(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	ctx := context.Background()
	_ = e.srv.Store.SaveActivity(ctx, &jobs.Activity{StravaID: "77", Name: "Run", Start: time.Now().Add(-3 * time.Hour), Duration: time.Hour, Status: jobs.StatusFailed, Error: "boom"})
	e.srv.FindActivity = func(context.Context, string) (*jobs.Activity, error) {
		t.Fatal("FindActivity should not be called for an id already in the store")
		return nil, nil
	}
	e.action("/actions/process", `{"processActivityId":"77"}`, c, nil)
	if len(e.jobs.got) != 1 || e.jobs.got[0].Activity.StravaID != "77" {
		t.Fatalf("jobs = %+v", e.jobs.got)
	}
}

func TestProcessActivityNotFound(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	e.srv.FindActivity = func(context.Context, string) (*jobs.Activity, error) { return nil, nil }
	if w := e.action("/actions/process", `{"processActivityId":"9999"}`, c, nil); !strings.Contains(w.Body.String(), "No activity with that id") {
		t.Errorf("not found: %s", w.Body)
	}
	if len(e.jobs.got) != 0 {
		t.Errorf("jobs = %+v", e.jobs.got)
	}
}

func TestProcessActivitySessionExpired(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	e.srv.FindActivity = func(context.Context, string) (*jobs.Activity, error) { return nil, jobs.ErrSessionExpired }
	if w := e.action("/actions/process", `{"processActivityId":"9999"}`, c, nil); !strings.Contains(w.Body.String(), "expired") {
		t.Errorf("expired: %s", w.Body)
	}
}

func TestUserTextIsEscaped(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	evil := `<script>alert(1)</script>`
	ctx := context.Background()
	_ = e.srv.Store.SaveActivity(ctx, &jobs.Activity{StravaID: "5", Name: evil, Sport: `"><img src=x onerror=alert(2)>`,
		Start: time.Now().Add(-3 * time.Hour), Duration: time.Hour, Status: jobs.StatusFailed, Error: evil})
	_ = e.srv.Store.RecordEvent(ctx, jobs.Event{Type: jobs.EventStravaFailed, Severity: "error", Message: evil, StravaID: "5"})

	for _, p := range []string{"/", "/activity/5", "/events"} {
		body := e.get(t, p, c).Body.String()
		if strings.Contains(body, "<script>alert") || strings.Contains(body, "<img src=x") {
			t.Errorf("%s renders unescaped user text", p)
		}
	}
}

func TestActivityPageLinksToStrava(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	ctx := context.Background()
	_ = e.srv.Store.SaveActivity(ctx, &jobs.Activity{StravaID: "20156391590", Name: "Run",
		Start: time.Now().Add(-3 * time.Hour), Duration: time.Hour, Status: jobs.StatusDone})
	body := e.get(t, "/activity/20156391590", c).Body.String()
	if !strings.Contains(body, `href="https://www.strava.com/activities/20156391590"`) {
		t.Errorf("no Strava link: %s", body)
	}
}

func TestNotifyTest(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	if w := e.action("/actions/notify/test", "{}", c, nil); !strings.Contains(w.Body.String(), "not available") {
		t.Errorf("without SendTest: %s", w.Body)
	}
	e.srv.SendTest = func(context.Context) error { return errors.New("ntfy: HTTP 500") }
	if w := e.action("/actions/notify/test", "{}", c, nil); !strings.Contains(w.Body.String(), "ntfy: HTTP 500") {
		t.Errorf("failure not shown: %s", w.Body)
	}
	e.srv.SendTest = func(context.Context) error { return nil }
	if w := e.action("/actions/notify/test", "{}", c, nil); !strings.Contains(w.Body.String(), "Test notification sent") {
		t.Errorf("success not shown: %s", w.Body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.get(t, "/login", nil)
	h := w.Header()
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") == "" {
		t.Errorf("headers = %v", h)
	}
}

func TestJSQuote(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"plain":       "plain",
		"it's":        `it\'s`,
		`back\slash`:  `back\\slash`,
		"a\nb":        `a\nb`,
		"</script>x":  `\x3c/script>x`,
		"');alert(1)": `\');alert(1)`,
	} {
		if got := jsQuote(in); got != want {
			t.Errorf("jsQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokenNameCannotBreakOutOfScript(t *testing.T) {
	e := newEnv(t)
	c := e.login(t)
	e.action("/actions/tokens/create", `{"tokenName":"x');alert(1);('"}`, c, nil)
	body := e.get(t, "/tokens", c).Body.String()
	if strings.Contains(body, "alert(1);(&#39;") && !strings.Contains(body, `%27`) {
		t.Error("token name reaches the click handler unescaped")
	}
	if strings.Contains(body, `revoke/x'`) {
		t.Errorf("raw quote in handler: %s", body)
	}
}

func TestRestoreNeedsStoredOriginal(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	if err := e.srv.Store.SaveActivity(context.Background(), &jobs.Activity{StravaID: "88", Status: jobs.StatusDone}); err != nil {
		t.Fatal(err)
	}
	if w := e.action("/actions/restore/88", "{}", c, nil); !strings.Contains(w.Body.String(), "No original description") {
		t.Errorf("body = %s", w.Body.String())
	}
	if w := e.action("/actions/restore/x'1", "{}", c, nil); !strings.Contains(w.Body.String(), "not an activity id") {
		t.Errorf("body = %s", w.Body.String())
	}
	orig := "my text"
	_ = e.srv.Store.SaveActivity(context.Background(), &jobs.Activity{StravaID: "88", Status: jobs.StatusDone, Original: &orig})
	e.action("/actions/restore/88", "{}", c, nil)
	if len(e.jobs.got) != 1 || !e.jobs.got[0].Restore {
		t.Errorf("jobs = %+v", e.jobs.got)
	}
}

func loginAttempt(e *env, remote, xff string) int {
	form := url.Values{"email": {testEmail}, "password": {"wrong"}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", origin)
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	r.RemoteAddr = remote
	return e.do(r).Code
}

func TestLoginLimitIgnoresSpoofedForwardedFor(t *testing.T) {
	t.Parallel()
	e := newEnv(t) // no trusted proxies
	for i := range maxLoginFailures {
		loginAttempt(e, "203.0.113.9:1", fmt.Sprintf("198.51.100.%d", i))
	}
	if code := loginAttempt(e, "203.0.113.9:1", "198.51.100.99"); code != http.StatusTooManyRequests {
		t.Errorf("rotating X-Forwarded-For dodged the limit: %d", code)
	}
}

func TestLoginLimitPerClientBehindTrustedProxy(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.srv.Proxies, _ = clientip.Parse("10.0.0.1")
	for range maxLoginFailures {
		loginAttempt(e, "10.0.0.1:1", "198.51.100.1")
	}
	if code := loginAttempt(e, "10.0.0.1:1", "198.51.100.1"); code != http.StatusTooManyRequests {
		t.Errorf("abusive client not limited: %d", code)
	}
	if code := loginAttempt(e, "10.0.0.1:1", "198.51.100.2"); code == http.StatusTooManyRequests {
		t.Error("other client behind the same proxy was locked out")
	}
}

func TestLogoutInvalidatesTheTokenServerSide(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	if w := e.get(t, "/", c); w.Code != http.StatusOK {
		t.Fatalf("before logout = %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "/logout", nil)
	r.Header.Set("Origin", origin)
	r.AddCookie(c)
	if w := e.do(r); w.Code != http.StatusSeeOther {
		t.Fatalf("logout = %d", w.Code)
	}
	// A copy of the cookie taken before logout must no longer work.
	if w := e.get(t, "/", c); w.Code == http.StatusOK {
		t.Error("token still valid after logout")
	}
}

func TestSessionLifetime(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	if c.MaxAge != migrations.SessionSeconds {
		t.Errorf("cookie MaxAge = %d", c.MaxAge)
	}
	users, _ := e.srv.App.FindCollectionByNameOrId("users")
	if got := users.AuthToken.Duration; got != migrations.SessionSeconds {
		t.Errorf("token duration = %d", got)
	}
}

func TestPurgeNeedsConfirmation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	_ = e.srv.Store.SaveActivity(context.Background(), &jobs.Activity{StravaID: "5", Status: jobs.StatusDone})

	if w := e.action("/actions/data/purge", `{"purgeConfirm":"delete"}`, c, nil); !strings.Contains(w.Body.String(), "Type DELETE") {
		t.Errorf("body = %s", w.Body.String())
	}
	if a, _ := e.srv.Store.Activity(context.Background(), "5"); a == nil {
		t.Fatal("activity deleted without confirmation")
	}
	if w := e.action("/actions/data/purge", `{"purgeConfirm":"DELETE"}`, c, nil); !strings.Contains(w.Body.String(), "Deleted 0 readings, 1 activities") {
		t.Errorf("body = %s", w.Body.String())
	}
	if a, _ := e.srv.Store.Activity(context.Background(), "5"); a != nil {
		t.Error("activity still there")
	}
}

func TestExportNeedsLoginAndSetsHeaders(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if w := e.get(t, "/export/samples.csv", nil); w.Code == http.StatusOK {
		t.Errorf("anonymous export = %d", w.Code)
	}
	c := e.login(t)
	w := e.get(t, "/export/activities.csv", c)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("code=%d headers=%v", w.Code, w.Header())
	}
}

func TestRetentionValidation(t *testing.T) {
	t.Parallel()
	v := settingsSignals{Unit: "mg/dL", RangeLow: 70, RangeHigh: 180, PollMin: 10, Lang: "en", DexcomRegion: "ous", RetentionDays: -1}
	if v.validate() == "" {
		t.Error("negative retention accepted")
	}
	v.RetentionDays = 3651
	if v.validate() == "" {
		t.Error("huge retention accepted")
	}
	v.RetentionDays = 0
	if msg := v.validate(); msg != "" {
		t.Errorf("0 rejected: %s", msg)
	}
}

func TestLoginFailureKeepsEmail(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	form := url.Values{"email": {testEmail}, "password": {"wrong"}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", origin)
	w := e.do(r)
	if !strings.Contains(w.Body.String(), `value="`+testEmail+`"`) {
		t.Errorf("email not redisplayed:\n%s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `value="wrong"`) {
		t.Error("password redisplayed")
	}
}

func TestNavBadgeShowsRecentErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	w := e.get(t, "/", c)
	if strings.Contains(w.Body.String(), `data-variant="error"`) {
		t.Error("badge shown with no events")
	}
	ctx := context.Background()
	_ = e.srv.Store.RecordEvent(ctx, jobs.Event{Type: jobs.EventSessionExpired, Severity: "error", Message: "x"})
	_ = e.srv.Store.RecordEvent(ctx, jobs.Event{Type: jobs.EventGlucoseUnavailable, Severity: "warning", Message: "y"})
	w = e.get(t, "/settings", c)
	if !strings.Contains(w.Body.String(), `data-variant="error">1<`) {
		t.Errorf("badge missing or wrong count:\n%s", w.Body.String())
	}
}

func TestActionDexcomTestUnavailableByDefault(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	w := e.action("/actions/dexcom/test", "{}", c, nil)
	if !strings.Contains(w.Body.String(), "not available") {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestActionDexcomTest(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	e.srv.GlucoseTest = func(context.Context) error { return errors.New("bad login") }
	if w := e.action("/actions/dexcom/test", "{}", c, nil); !strings.Contains(w.Body.String(), "bad login") {
		t.Errorf("body = %s", w.Body.String())
	}
	e.srv.GlucoseTest = func(context.Context) error { return nil }
	if w := e.action("/actions/dexcom/test", "{}", c, nil); !strings.Contains(w.Body.String(), "accepted") {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestActionStravaLoginValidatesAndReportsBlocked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)

	if w := e.action("/actions/strava/login", `{"loginEmail":"","loginPassword":""}`, c, nil); !strings.Contains(w.Body.String(), "Enter an email") {
		t.Errorf("empty form: %s", w.Body.String())
	}
	if w := e.action("/actions/strava/login", `{"loginEmail":"a@example.test","loginPassword":"x"}`, c, nil); !strings.Contains(w.Body.String(), "not available") {
		t.Errorf("no StravaLogin wired: %s", w.Body.String())
	}

	var gotEmail, gotPassword string
	e.srv.StravaLogin = func(_ context.Context, email, password string) error {
		gotEmail, gotPassword = email, password
		return &strava.LoginError{Reason: strava.BlockedChallenge, Detail: "needs a code"}
	}
	w := e.action("/actions/strava/login", `{"loginEmail":"a@example.test","loginPassword":"hunter2"}`, c, nil)
	if !strings.Contains(w.Body.String(), "needs a code") {
		t.Errorf("body = %s", w.Body.String())
	}
	if gotEmail != "a@example.test" || gotPassword != "hunter2" {
		t.Errorf("got %q %q", gotEmail, gotPassword)
	}

	e.srv.StravaLogin = func(context.Context, string, string) error { return nil }
	if w := e.action("/actions/strava/login", `{"loginEmail":"a@example.test","loginPassword":"hunter2"}`, c, nil); !strings.Contains(w.Body.String(), "Signed in") {
		t.Errorf("success body = %s", w.Body.String())
	}
}
