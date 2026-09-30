package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type memPush struct {
	mu   sync.Mutex
	subs []PushSub
	ok   map[string]string // endpoint -> last error ("" = success)
}

func (m *memPush) PushSubscriptions(context.Context) ([]PushSub, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]PushSub(nil), m.subs...), nil
}

func (m *memPush) PushResult(_ context.Context, ep string, _ time.Time, errText string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ok == nil {
		m.ok = map[string]string{}
	}
	m.ok[ep] = errText
	return nil
}

func (m *memPush) DeletePushSubscription(_ context.Context, ep string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.subs {
		if s.Endpoint == ep {
			m.subs = append(m.subs[:i], m.subs[i+1:]...)
			break
		}
	}
	return nil
}

type memSecrets map[string]string

func (m memSecrets) Get(n string) (string, bool, error) { v, ok := m[n]; return v, ok, nil }
func (m memSecrets) Set(n, v string) error              { m[n] = v; return nil }

func browserKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := make([]byte, 16)
	_, _ = rand.Read(a)
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(a)
}

type seen struct {
	auth, enc, ttl, urgency string
	size                    int64
}

func TestWebPushSend(t *testing.T) {
	var mu sync.Mutex
	got := map[string]seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got[r.URL.Path] = seen{r.Header.Get("Authorization"), r.Header.Get("Content-Encoding"), r.Header.Get("TTL"), r.Header.Get("Urgency"), r.ContentLength}
		mu.Unlock()
		switch r.URL.Path {
		case "/gone":
			w.WriteHeader(http.StatusGone)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/broken":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer srv.Close()

	priv, pub, err := EnsureVAPID(memSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	dh, auth := browserKeys(t)
	st := &memPush{}
	for _, p := range []string{"/good", "/gone", "/missing", "/broken"} {
		st.subs = append(st.subs, PushSub{Endpoint: srv.URL + p, P256dh: dh, Auth: auth})
	}
	w := &WebPush{Store: st, Private: priv, Public: pub, Subject: "https://glucava.example", Client: srv.Client()}

	err = w.Send(context.Background(), Message{Type: "strava_failed", Title: "Upload failed", Body: "Strava said no\nsession expired", Severity: "error", StravaID: "42"})
	if err != nil {
		t.Fatalf("one device accepted, Send = %v", err)
	}

	g := got["/good"]
	if !strings.HasPrefix(g.auth, "vapid t=") || !strings.Contains(g.auth, ", k="+pub) {
		t.Errorf("Authorization = %q", g.auth)
	}
	if g.enc != "aes128gcm" {
		t.Errorf("Content-Encoding = %q", g.enc)
	}
	if g.ttl != "14400" || g.urgency != "high" {
		t.Errorf("TTL/Urgency = %q/%q", g.ttl, g.urgency)
	}
	if g.size <= 0 || g.size > 4096 {
		t.Errorf("body size = %d, want within one 4 KB record", g.size)
	}

	if len(st.subs) != 2 {
		t.Errorf("after 404/410 cleanup %d devices remain, want 2", len(st.subs))
	}
	if e, ok := st.ok[srv.URL+"/good"]; !ok || e != "" {
		t.Errorf("good result = %q, %v", e, ok)
	}
	if e := st.ok[srv.URL+"/broken"]; !strings.Contains(e, "HTTP 500") {
		t.Errorf("broken result = %q", e)
	}
	if strings.Contains(st.ok[srv.URL+"/broken"], srv.URL) {
		t.Errorf("recorded error leaks the endpoint URL: %q", st.ok[srv.URL+"/broken"])
	}
}

func TestWebPushAllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer srv.Close()
	priv, pub, _ := EnsureVAPID(memSecrets{})
	dh, auth := browserKeys(t)
	st := &memPush{subs: []PushSub{{Endpoint: srv.URL + "/x", P256dh: dh, Auth: auth}}}
	w := &WebPush{Store: st, Private: priv, Public: pub, Client: srv.Client()}
	if err := w.Send(context.Background(), Message{Type: "strava_failed", Title: "t", Body: "b"}); err == nil {
		t.Error("every device failed but Send succeeded")
	}
	if len(st.subs) != 1 {
		t.Error("a server error must not remove the device")
	}
}

func TestWebPushNoDevicesAndWants(t *testing.T) {
	priv, pub, _ := EnsureVAPID(memSecrets{})
	w := &WebPush{Store: &memPush{}, Private: priv, Public: pub}
	if err := w.Send(context.Background(), Message{Type: "strava_failed"}); err == nil {
		t.Error("no devices: want an error so the event stays pending")
	}
	w.Wants = func(string) bool { return false }
	if err := w.Send(context.Background(), Message{Type: "strava_failed"}); err != nil {
		t.Errorf("filtered message: %v", err)
	}
}

func TestEnsureVAPIDStable(t *testing.T) {
	s := memSecrets{}
	p1, pub1, err := EnsureVAPID(s)
	if err != nil || p1 == "" || pub1 == "" {
		t.Fatal(p1, pub1, err)
	}
	p2, pub2, _ := EnsureVAPID(s)
	if p1 != p2 || pub1 != pub2 {
		t.Error("second call generated new keys")
	}
	if raw, _ := base64.RawURLEncoding.DecodeString(pub1); len(raw) != 65 || raw[0] != 4 {
		t.Errorf("public key is not an uncompressed point: %d bytes", len(raw))
	}
	s["vapid_private"] = "garbage"
	p3, _, err := EnsureVAPID(s)
	if err != nil || p3 == "garbage" {
		t.Errorf("unusable key not replaced: %q %v", p3, err)
	}
}

func TestPushPayloadSmall(t *testing.T) {
	p := pushPayload(Message{Type: TypeActivitySummary, Title: "Run", Body: strings.Repeat("word ", 500), StravaID: "9"})
	if len([]rune(p.Body)) > maxPayloadBody || !strings.HasSuffix(p.Body, "…") {
		t.Errorf("body not clipped: %d", len([]rune(p.Body)))
	}
	if !strings.HasPrefix(p.URL, "/") || strings.Contains(p.URL, "://") {
		t.Errorf("URL must be a path: %q", p.URL)
	}
	if p.Tag == "" {
		t.Error("no tag")
	}
}

func TestPushTiming(t *testing.T) {
	for _, tc := range []struct {
		m    Message
		ttl  int
		urg  string
		name string
	}{
		{Message{Type: TypeTest}, 60, "high", "test"},
		{Message{Type: TypeActivitySummary}, 21600, "normal", "activity"},
		{Message{Type: TypeWeeklySummary}, 86400, "low", "weekly"},
		{Message{Type: "strava_failed", Severity: "error"}, 14400, "high", "failure"},
	} {
		ttl, urg := pushTiming(tc.m)
		if ttl != tc.ttl || string(urg) != tc.urg {
			t.Errorf("%s: %d/%s, want %d/%s", tc.name, ttl, urg, tc.ttl, tc.urg)
		}
	}
}

func TestValidPushEndpoint(t *testing.T) {
	for _, ok := range []string{"https://fcm.googleapis.com/fcm/send/abc", "https://updates.push.services.mozilla.com/wpush/v2/x", "https://web.push.apple.com/Q1"} {
		if err := ValidPushEndpoint(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "http://fcm.googleapis.com/x", "https://localhost/x", "https://127.0.0.1/x", "https://10.0.0.5/x",
		"https://192.168.1.1/x", "https://[::1]/x", "https://169.254.169.254/x", "https://100.64.1.1/x", "https://nas.local/x",
		"https://user:pw@fcm.googleapis.com/x", "ftp://x/y", "https://" + strings.Repeat("a", 2100)} {
		if ValidPushEndpoint(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidPushKeys(t *testing.T) {
	dh, auth := browserKeys(t)
	if err := ValidPushKeys(dh, auth); err != nil {
		t.Errorf("valid keys: %v", err)
	}
	if ValidPushKeys("AAAA", auth) == nil || ValidPushKeys(dh, "AAAA") == nil {
		t.Error("short keys accepted")
	}
	bad := base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
	if ValidPushKeys(bad, auth) == nil {
		t.Error("point off the curve accepted")
	}
}

func TestSafePushClientRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	if resp, err := safePushClient().Do(req); err == nil {
		_ = resp.Body.Close()
		t.Error("client connected to a loopback address")
	}
}

func TestSubject(t *testing.T) {
	if Subject("https://g.example", "a@b.c") != "https://g.example" || Subject("http://g", "a@b.c") != "a@b.c" || Subject("", "") != fallbackSubject {
		t.Error("subject choice wrong")
	}
}
