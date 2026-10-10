package glucose

import (
	"context"
	"crypto/sha1" //nolint:gosec // matches the Nightscout protocol under test
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryIsConsistent(t *testing.T) {
	for _, id := range []string{"dexcom", "librelinkup", "nightscout"} {
		if _, ok := Lookup(id); !ok {
			t.Errorf("source %s is not registered", id)
		}
	}
	keys := map[string]string{}
	for _, d := range All() {
		for _, f := range d.Fields {
			name := SettingKey(d.ID, f)
			if f.Secret() {
				name = SecretName(d.ID, f)
			}
			if owner, dup := keys[name]; dup {
				t.Errorf("%s of %s collides with %s", name, d.ID, owner)
			}
			keys[name] = d.ID
		}
	}
	// The pre-registry vault name stays, or an upgrade would lose the Dexcom password.
	d, _ := Lookup("dexcom")
	if SecretName("dexcom", d.Fields[2]) != "dexcom_password" {
		t.Error("dexcom password vault name changed")
	}
}

func TestValidate(t *testing.T) {
	get := func(m map[string]string) func(Field) string { return func(f Field) string { return m[f.Key] } }
	if _, ok := Validate("dexcom", get(map[string]string{"region": "mars"})); ok {
		t.Error("unknown region accepted")
	}
	if _, ok := Validate("dexcom", get(map[string]string{"region": "us"})); !ok {
		t.Error("valid region rejected")
	}
	for _, bad := range []string{"ftp://x", "not a url", "http://"} {
		if _, ok := Validate("nightscout", get(map[string]string{"url": bad})); ok {
			t.Errorf("nightscout url %q accepted", bad)
		}
	}
	if _, ok := Validate("nightscout", get(map[string]string{"url": "https://ns.example"})); !ok {
		t.Error("good url rejected")
	}
	if _, ok := Validate("nope", get(nil)); ok {
		t.Error("unknown source accepted")
	}
	ns, _ := Lookup("nightscout")
	if f, missing := MissingRequired(ns, get(nil)); !missing || f.Key != "url" {
		t.Errorf("missing = %v %v", f.Key, missing)
	}
}

func TestNightscout(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var gotQuery, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotSecret = r.URL.RawQuery, r.Header.Get("api-secret")
		if r.URL.Path != "/api/v1/entries/sgv.json" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("token") == "bad" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		ms := func(m int) int64 { return t0.Add(time.Duration(m) * time.Minute).UnixMilli() }
		_, _ = w.Write([]byte(`[
			{"type":"sgv","sgv":120,"date":` + itoa64(ms(10)) + `},
			{"type":"sgv","sgv":100,"date":` + itoa64(ms(5)) + `},
			{"type":"mbg","sgv":0,"mbg":99,"date":` + itoa64(ms(6)) + `},
			{"type":"sgv","sgv":999,"date":` + itoa64(ms(500)) + `}]`))
	}))
	defer srv.Close()

	n := &Nightscout{BaseURL: srv.URL + "/", Token: "tok", APISecret: "s3cret"}
	got, err := n.Samples(context.Background(), t0, t0.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != 100 || got[1].Value != 120 || !got[0].Time.Before(got[1].Time) {
		t.Errorf("samples = %+v, want the two sgv values in the window, oldest first", got)
	}
	sum := sha1.Sum([]byte("s3cret")) //nolint:gosec
	if gotSecret != hex.EncodeToString(sum[:]) || !strings.Contains(gotQuery, "token=tok") {
		t.Errorf("auth not sent: secret=%q query=%q", gotSecret, gotQuery)
	}

	if _, err := (&Nightscout{BaseURL: srv.URL, Token: "bad"}).Samples(context.Background(), t0, t0.Add(time.Hour)); !errors.Is(err, ErrAuth) {
		t.Errorf("401 = %v, want ErrAuth", err)
	}

	// A transport error must not leak the token from the URL.
	srv.Close()
	_, err = (&Nightscout{BaseURL: srv.URL, Token: "supersecrettoken"}).Samples(context.Background(), t0, t0.Add(time.Hour))
	if err == nil || strings.Contains(err.Error(), "supersecrettoken") {
		t.Errorf("error = %v, must fail without the token in it", err)
	}
}

func TestLibreLinkUp(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var logins, expire atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("product") != "llu.android" || r.Header.Get("version") == "" {
			http.Error(w, "headers", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/llu/auth/login":
			n := logins.Add(1)
			if n == 1 { // first answer redirects to another region (here: the same test host)
				_, _ = w.Write([]byte(`{"status":0,"data":{"redirect":true,"region":"eu"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":0,"data":{"user":{"id":"u1"},"authTicket":{"token":"T` + itoa64(int64(n)) + `"}}}`))
		case "/llu/connections":
			if r.Header.Get("Authorization") == "" || r.Header.Get("Account-Id") == "" {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"status":0,"data":[{"patientId":"p1"}]}`))
		case "/llu/connections/p1/graph":
			if expire.Load() == 1 { // the session ends once
				expire.Store(0)
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"status":0,"data":{
				"connection":{"glucoseMeasurement":{"ValueInMgPerDl":130,"FactoryTimestamp":"10/1/2026 11:55:00 AM"}},
				"graphData":[{"ValueInMgPerDl":110,"FactoryTimestamp":"10/1/2026 11:40:00 AM"},
				             {"ValueInMgPerDl":130,"FactoryTimestamp":"10/1/2026 11:55:00 AM"},
				             {"ValueInMgPerDl":90,"FactoryTimestamp":"10/1/2026 9:00:00 AM"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	l := NewLibreLinkUp("global", "a@b.c", "pw")
	l.BaseURL, l.Now = srv.URL, func() time.Time { return now }
	got, err := l.Samples(context.Background(), now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != 110 || got[1].Value != 130 || !got[1].Time.Equal(time.Date(2026, 10, 1, 11, 55, 0, 0, time.UTC)) {
		t.Errorf("samples = %+v, want 110 and 130 once each (latest is also in the graph)", got)
	}
	if logins.Load() != 2 {
		t.Errorf("logins = %d, want 2 (one redirect)", logins.Load())
	}

	expire.Store(1)
	if got, err = l.Samples(context.Background(), now.Add(-time.Hour), now); err != nil || len(got) != 2 {
		t.Errorf("after session expiry: %v, %d samples", err, len(got))
	}
	if logins.Load() != 3 {
		t.Errorf("logins = %d, want a fresh login after the 401", logins.Load())
	}

	if _, err := l.Samples(context.Background(), now.Add(-13*time.Hour), now); !errors.Is(err, ErrTooOld) {
		t.Errorf("13h window = %v, want ErrTooOld", err)
	}
}

func TestLibreLinkUpBadPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":2,"error":{"message":"notAuthenticated"}}`))
	}))
	defer srv.Close()
	l := NewLibreLinkUp("global", "a@b.c", "wrong")
	l.BaseURL = srv.URL
	if _, err := l.Samples(context.Background(), time.Now().Add(-time.Hour), time.Now()); !errors.Is(err, ErrAuth) {
		t.Errorf("bad password = %v, want ErrAuth", err)
	}
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
