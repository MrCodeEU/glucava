package strava

import (
	"testing"
	"time"
)

func TestParseHeader(t *testing.T) {
	got, err := ParseCookies("Cookie: _strava4_session=abc123; other=x=y")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "_strava4_session" || got[0].Value != "abc123" || got[1].Value != "x=y" {
		t.Errorf("got %+v", got)
	}
	if !HasSession(got) {
		t.Error("HasSession = false")
	}
}

func TestParseJSONFiltersDomains(t *testing.T) {
	in := `[
	 {"name":"_strava4_session","value":"s","domain":".strava.com","path":"/","secure":true,"httpOnly":true,"expirationDate":1800000000.5},
	 {"name":"tracker","value":"t","domain":".google.com","path":"/"}
	]`
	got, err := ParseCookies(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Secure || !got[0].HTTPOnly || got[0].Expires.Unix() != 1800000000 {
		t.Errorf("got %+v", got)
	}
}

func TestParseNetscape(t *testing.T) {
	in := "# Netscape HTTP Cookie File\n" +
		"#HttpOnly_.strava.com\tTRUE\t/\tTRUE\t1800000000\t_strava4_session\tsess\n" +
		".strava.com\tTRUE\t/\tFALSE\t0\tsp\t1\n" +
		".example.com\tTRUE\t/\tFALSE\t0\tno\t1\n"
	got, err := ParseCookies(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].HTTPOnly || !got[0].Secure || got[1].Expires != (time.Time{}) {
		t.Errorf("got %+v", got)
	}
}

func TestParseErrors(t *testing.T) {
	for name, in := range map[string]string{
		"empty":          "  ",
		"bad json":       "[{",
		"no strava":      `[{"name":"a","value":"b","domain":".example.com"}]`,
		"bad header":     "justtext",
		"short netscape": "a\tb\tc",
	} {
		if _, err := ParseCookies(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := []Cookie{{Name: "a", Value: "b", Domain: ".strava.com", Expires: time.Unix(1800000000, 0).UTC()}}
	s, err := EncodeCookies(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeCookies(s)
	if err != nil || len(out) != 1 || !out[0].Expires.Equal(in[0].Expires) || out[0].Value != "b" {
		t.Errorf("out = %+v, %v", out, err)
	}
}
