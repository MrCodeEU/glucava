package web

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/MrCodeEU/glucava/internal/notify"
)

func testPushKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := make([]byte, 16)
	_, _ = rand.Read(a)
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(a)
}

// subscribeBody is the JSON Datastar posts: the signals, with pushSub holding
// the browser's subscription as text.
func subscribeBody(endpoint, p256dh, auth string) string {
	inner, _ := json.Marshal(map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": p256dh, "auth": auth}})
	b, _ := json.Marshal(map[string]string{"pushSub": string(inner)})
	return string(b)
}

func TestManifestIsPublicAndValid(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.get(t, "/manifest.webmanifest", nil) // no session
	if w.Code != http.StatusOK {
		t.Fatalf("manifest = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/manifest+json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	if raw["name"] != "glucava" || raw["start_url"] != "/" || raw["scope"] != "/" || raw["display"] != "standalone" {
		t.Errorf("manifest basics = %v", raw)
	}
	icons, _ := raw["icons"].([]any)
	purposes := map[string]bool{}
	for _, i := range icons {
		ic := i.(map[string]any)
		src := ic["src"].(string)
		purposes[ic["sizes"].(string)+"/"+ic["purpose"].(string)] = true
		if got := e.get(t, src, nil); got.Code != http.StatusOK || !strings.HasPrefix(got.Header().Get("Content-Type"), "image/png") {
			t.Errorf("icon %s = %d %s", src, got.Code, got.Header().Get("Content-Type"))
		}
	}
	for _, want := range []string{"192x192/any", "512x512/any", "512x512/maskable"} {
		if !purposes[want] {
			t.Errorf("manifest lacks icon %s", want)
		}
	}
}

func TestServiceWorkerServedAtRoot(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.get(t, "/sw.js", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("sw.js = %d", w.Code)
	}
	h := w.Header()
	if !strings.Contains(h.Get("Content-Type"), "javascript") || h.Get("Cache-Control") != "no-cache" || h.Get("Service-Worker-Allowed") != "/" {
		t.Errorf("headers = %v", h)
	}
	body := w.Body.String()
	if strings.Contains(body, "__BUILD__") || strings.Contains(body, "__ASSETS__") {
		t.Error("placeholders left in the worker")
	}
	for _, want := range []string{`const BUILD = "test";`, "/static/app.css?v=test", "addEventListener('push'", "addEventListener('notificationclick'", "u.origin === self.location.origin", "caches.delete"} {
		if !strings.Contains(body, want) {
			t.Errorf("service worker lacks %q", want)
		}
	}
	// The worker must never cache pages or data: only /static/ is answered from the cache.
	if strings.Contains(body, "cache.put") || strings.Contains(body, "cache.add(") {
		t.Error("service worker caches at runtime")
	}
}

func TestPWAHeadAndNoInlineScripts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	for _, p := range []string{"/login", "/settings", "/"} {
		var ck *http.Cookie
		if p != "/login" {
			ck = c
		}
		body := e.get(t, p, ck).Body.String()
		for _, want := range []string{`rel="manifest" href="/manifest.webmanifest"`, `name="theme-color"`, `rel="apple-touch-icon"`, `/static/pwa.js?v=test`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %s", p, want)
			}
		}
		if strings.Contains(body, "<script>") {
			t.Errorf("%s has an inline script, which the CSP forbids", p)
		}
	}
	for _, f := range []string{"/static/pwa.js", "/static/sw.js", "/static/badge-96.png", "/static/apple-touch-icon.png"} {
		if w := e.get(t, f, nil); w.Code != http.StatusOK {
			t.Errorf("%s = %d", f, w.Code)
		}
	}
}

// TestCSPStillCoversWorkerAndManifest: the CSP has no worker-src or
// manifest-src, so they fall back to script-src and default-src 'self',
// which is what the same-origin worker and manifest need. Adding sources
// here would widen the policy for nothing.
func TestCSPStillCoversWorkerAndManifest(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"worker-src", "manifest-src", "'unsafe-inline' 'unsafe-eval'", "http:", "https:"} {
		if strings.Contains(csp, bad) {
			t.Errorf("csp contains %q", bad)
		}
	}
	if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "script-src 'self' 'unsafe-eval'") {
		t.Errorf("csp = %s", csp)
	}
	e := newEnv(t)
	if got := e.get(t, "/sw.js", nil).Header().Get("Content-Security-Policy"); got != csp {
		t.Errorf("sw.js CSP = %q", got)
	}
}

func TestPushRoutesCoveredByRoutes(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"/manifest.webmanifest", "/sw.js", "/actions/push/subscribe", "/actions/push/unsubscribe", "/actions/push/test", "/actions/push/remove/{id}"} {
		if !coveredByRoutes(p) {
			t.Errorf("%q missing from web.Routes", p)
		}
	}
}

func TestPushActionsNeedSessionAndSameOrigin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	for _, p := range []string{"/actions/push/subscribe", "/actions/push/unsubscribe", "/actions/push/test", "/actions/push/remove/x"} {
		if w := e.action(p, "{}", nil, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s without session = %d", p, w.Code)
		}
		if w := e.action(p, "{}", c, map[string]string{"Origin": "http://evil.example"}); w.Code != http.StatusForbidden {
			t.Errorf("%s cross-site = %d", p, w.Code)
		}
	}
}

func TestPushSubscribeListRemove(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	dh, auth := testPushKeys(t)

	w := e.action("/actions/push/subscribe", subscribeBody("https://fcm.googleapis.com/fcm/send/abc", dh, auth), c, map[string]string{"User-Agent": "Mozilla/5.0 (Linux; Android 14) Chrome/130.0 Mobile Safari/537.36"})
	if !strings.Contains(w.Body.String(), "get push notifications") {
		t.Fatalf("subscribe = %s", w.Body)
	}
	subs, _ := e.srv.Store.PushSubscriptions(context.Background())
	if len(subs) != 1 || subs[0].Endpoint != "https://fcm.googleapis.com/fcm/send/abc" {
		t.Fatalf("subs = %+v", subs)
	}
	if !strings.Contains(w.Body.String(), `id="push-devices"`) || !strings.Contains(w.Body.String(), "Chrome on Android") {
		t.Errorf("device list not patched: %s", w.Body)
	}

	// The settings card lists it and carries the public key for pwa.js.
	page := e.get(t, "/settings", c).Body.String()
	_, pub, _ := notify.EnsureVAPID(e.srv.Vault)
	for _, want := range []string{`id="push-card"`, `data-vapid="` + pub + `"`, "Enable on this device", "Chrome on Android", "Send test push", "Failure alerts", `data-bind="pushAlerts"`, `data-bind="pushSummaries"`} {
		if !strings.Contains(page, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
	// The private key never reaches a page.
	priv, _, _ := e.srv.Vault.Get("vapid_private")
	if priv == "" || strings.Contains(page, priv) {
		t.Errorf("private key stored=%v leaked=%v", priv != "", strings.Contains(page, priv))
	}

	e.action("/actions/push/remove/"+subs[0].ID, "{}", c, nil)
	if subs, _ = e.srv.Store.PushSubscriptions(context.Background()); len(subs) != 0 {
		t.Errorf("after remove: %+v", subs)
	}

	e.action("/actions/push/subscribe", subscribeBody("https://fcm.googleapis.com/fcm/send/abc", dh, auth), c, nil)
	e.action("/actions/push/unsubscribe", `{"pushSub":"https://fcm.googleapis.com/fcm/send/abc"}`, c, nil)
	if subs, _ = e.srv.Store.PushSubscriptions(context.Background()); len(subs) != 0 {
		t.Errorf("after unsubscribe: %+v", subs)
	}
}

func TestPushSubscribeRejectsBadInput(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	dh, auth := testPushKeys(t)
	for name, body := range map[string]string{
		"http endpoint":    subscribeBody("http://push.example/x", dh, auth),
		"loopback":         subscribeBody("https://127.0.0.1/x", dh, auth),
		"metadata address": subscribeBody("https://169.254.169.254/latest", dh, auth),
		"bad key":          subscribeBody("https://push.example/x", "AAAA", auth),
		"bad auth":         subscribeBody("https://push.example/x", dh, "AAAA"),
		"not json":         `{"pushSub":"nope"}`,
		"empty":            `{}`,
	} {
		w := e.action("/actions/push/subscribe", body, c, nil)
		if !strings.Contains(w.Body.String(), `data-variant="error"`) {
			t.Errorf("%s: no error toast: %s", name, w.Body)
		}
	}
	if subs, _ := e.srv.Store.PushSubscriptions(context.Background()); len(subs) != 0 {
		t.Errorf("rejected input stored: %+v", subs)
	}
}

func TestPushTestUsesDemoClient(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	if w := e.action("/actions/push/test", "{}", c, nil); !strings.Contains(w.Body.String(), "no device is subscribed") {
		t.Errorf("no devices: %s", w.Body)
	}
	e.srv.Demo = true // the fake client accepts without contacting the endpoint
	dh, auth := testPushKeys(t)
	e.action("/actions/push/subscribe", subscribeBody("https://push.example/demo", dh, auth), c, nil)
	w := e.action("/actions/push/test", "{}", c, nil)
	if !strings.Contains(w.Body.String(), "Test push sent") {
		t.Fatalf("test = %s", w.Body)
	}
	subs, _ := e.srv.Store.PushSubscriptions(context.Background())
	if len(subs) != 1 || subs[0].LastOK.IsZero() {
		t.Errorf("success not recorded: %+v", subs)
	}
}

func TestPushSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	on := strings.Replace(validSettings, `"gapAlertHours":6,`, `"gapAlertHours":6,"pushAlerts":false,"pushSummaries":true,`, 1)
	e.action("/actions/settings", on, c, nil)
	cfg, _ := e.srv.Store.LoadConfig()
	if cfg.PushAlerts || !cfg.PushSummaries {
		t.Errorf("saved = alerts %v summaries %v", cfg.PushAlerts, cfg.PushSummaries)
	}
	page := e.get(t, "/settings", c).Body.String()
	if !strings.Contains(html.UnescapeString(page), `"pushAlerts":false`) || !strings.Contains(html.UnescapeString(page), `"pushSummaries":true`) {
		t.Error("settings signals do not carry the saved push switches")
	}
}

func TestDeviceName(t *testing.T) {
	t.Parallel()
	for ua, want := range map[string]string{
		"": "Unknown device",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 Version/17.4 Mobile/15E148 Safari/604.1": "Safari on iPhone",
		"Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0":                                              "Firefox on Linux",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0 Safari/537.36 Edg/130.0":                   "Edge on Windows",
	} {
		if got := deviceName(ua); got != want {
			t.Errorf("deviceName(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestSafeRenderOfPushCardXSS(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	dh, auth := testPushKeys(t)
	e.action("/actions/push/subscribe", subscribeBody("https://push.example/x", dh, auth), c, map[string]string{"User-Agent": `<script>alert(1)</script> Firefox/1`})
	page := e.get(t, "/settings", c).Body.String()
	if strings.Contains(page, "<script>alert(1)") {
		t.Error("user agent reaches the page unescaped")
	}
}
