package clientip

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(remote string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestIP(t *testing.T) {
	proxies, err := Parse("10.0.0.0/8, 192.168.1.5, ::1")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		r    *Resolver
		req  *http.Request
		want string
	}{
		"no proxies ignores header": {&Resolver{}, req("1.2.3.4:5", map[string]string{"X-Forwarded-For": "9.9.9.9"}), "1.2.3.4"},
		"nil resolver":              {nil, req("1.2.3.4:5", nil), "1.2.3.4"},
		"untrusted peer spoofs":     {proxies, req("1.2.3.4:5", map[string]string{"X-Forwarded-For": "9.9.9.9"}), "1.2.3.4"},
		"trusted proxy":             {proxies, req("10.1.1.1:5", map[string]string{"X-Forwarded-For": "7.7.7.7"}), "7.7.7.7"},
		"client prepends fake":      {proxies, req("10.1.1.1:5", map[string]string{"X-Forwarded-For": "6.6.6.6, 7.7.7.7"}), "7.7.7.7"},
		"chain of proxies":          {proxies, req("10.1.1.1:5", map[string]string{"X-Forwarded-For": "7.7.7.7, 10.2.2.2, 192.168.1.5"}), "7.7.7.7"},
		"no header":                 {proxies, req("10.1.1.1:5", nil), "10.1.1.1"},
		"malformed header":          {proxies, req("10.1.1.1:5", map[string]string{"X-Forwarded-For": "not-an-ip"}), "10.1.1.1"},
		"ipv6 peer":                 {proxies, req("[::1]:5", map[string]string{"X-Forwarded-For": "2001:db8::1"}), "2001:db8::1"},
		"v4-mapped peer":            {proxies, req("[::ffff:10.1.1.1]:5", map[string]string{"X-Forwarded-For": "7.7.7.7"}), "7.7.7.7"},
	} {
		if got := tc.r.IP(tc.req); got != tc.want {
			t.Errorf("%s: IP = %q, want %q", name, got, tc.want)
		}
	}
}

func TestSecure(t *testing.T) {
	proxies, _ := Parse("10.0.0.1")
	if !proxies.Secure(req("10.0.0.1:1", map[string]string{"X-Forwarded-Proto": "https"})) {
		t.Error("trusted proxy https not honoured")
	}
	if proxies.Secure(req("1.2.3.4:1", map[string]string{"X-Forwarded-Proto": "https"})) {
		t.Error("untrusted peer must not set https")
	}
	if (&Resolver{}).Secure(req("10.0.0.1:1", map[string]string{"X-Forwarded-Proto": "https"})) {
		t.Error("empty resolver trusted a header")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse("10.0.0.0/8, nope"); err == nil {
		t.Error("expected an error")
	}
	if r, err := Parse(""); err != nil || r == nil {
		t.Errorf("empty list = %v, %v", r, err)
	}
}
