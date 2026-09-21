package strava

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cookie is a browser cookie. Domain may be empty; the writer then scopes the
// cookie to the Strava base URL.
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain,omitempty"`
	Path     string    `json:"path,omitempty"`
	Expires  time.Time `json:"expires,omitzero"` // zero means a session cookie
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"httpOnly,omitempty"`
}

// sessionCookie is the cookie Strava uses for a logged-in web session.
const sessionCookie = "_strava4_session"

// HasSession reports whether cookies include the Strava web session cookie.
func HasSession(cookies []Cookie) bool {
	for _, c := range cookies {
		if c.Name == sessionCookie && c.Value != "" {
			return true
		}
	}
	return false
}

// ParseCookies reads cookies pasted from a browser. It accepts three formats:
// a JSON array as exported by cookie extensions, a Netscape cookies.txt file, or
// a plain "name=value; name2=value2" header string.
func ParseCookies(text string) ([]Cookie, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("strava: no cookies given")
	}
	var (
		out []Cookie
		err error
	)
	switch {
	case strings.HasPrefix(text, "["):
		out, err = parseJSONCookies(text)
	case strings.Contains(text, "\t"):
		out, err = parseNetscape(text)
	default:
		out, err = parseHeader(text)
	}
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("strava: no cookies found in the pasted text")
	}
	return out, nil
}

func parseJSONCookies(text string) ([]Cookie, error) {
	var raw []struct {
		Name           string  `json:"name"`
		Value          string  `json:"value"`
		Domain         string  `json:"domain"`
		Path           string  `json:"path"`
		Secure         bool    `json:"secure"`
		HTTPOnly       bool    `json:"httpOnly"`
		ExpirationDate float64 `json:"expirationDate"`
		Expires        float64 `json:"expires"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("strava: cookie JSON: %w", err)
	}
	var out []Cookie
	for _, r := range raw {
		if r.Name == "" || !stravaDomain(r.Domain) {
			continue
		}
		c := Cookie{Name: r.Name, Value: r.Value, Domain: r.Domain, Path: r.Path, Secure: r.Secure, HTTPOnly: r.HTTPOnly}
		if exp := max(r.ExpirationDate, r.Expires); exp > 0 {
			c.Expires = time.Unix(int64(exp), 0).UTC()
		}
		out = append(out, c)
	}
	return out, nil
}

func parseNetscape(text string) ([]Cookie, error) {
	var out []Cookie
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		httpOnly := false
		if rest, ok := strings.CutPrefix(line, "#HttpOnly_"); ok {
			line, httpOnly = rest, true
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 7 {
			return nil, fmt.Errorf("strava: cookies.txt line has %d fields, want 7", len(f))
		}
		if !stravaDomain(f[0]) {
			continue
		}
		c := Cookie{Domain: f[0], Path: f[2], Secure: strings.EqualFold(f[3], "TRUE"), HTTPOnly: httpOnly, Name: f[5], Value: f[6]}
		if exp, err := strconv.ParseInt(f[4], 10, 64); err == nil && exp > 0 {
			c.Expires = time.Unix(exp, 0).UTC()
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

func parseHeader(text string) ([]Cookie, error) {
	text = strings.TrimPrefix(text, "Cookie:")
	var out []Cookie
	for _, part := range strings.Split(text, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("strava: cannot read cookie %q", strings.TrimSpace(part))
		}
		out = append(out, Cookie{Name: name, Value: value})
	}
	return out, nil
}

// stravaDomain reports whether a cookie domain belongs to strava.com.
func stravaDomain(d string) bool {
	d = strings.TrimPrefix(strings.ToLower(d), ".")
	return d == "strava.com" || strings.HasSuffix(d, ".strava.com")
}

// EncodeCookies serialises cookies for storage in the vault.
func EncodeCookies(c []Cookie) (string, error) {
	b, err := json.Marshal(c)
	return string(b), err
}

// DecodeCookies reverses EncodeCookies.
func DecodeCookies(s string) ([]Cookie, error) {
	var c []Cookie
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return nil, fmt.Errorf("strava: decode stored cookies: %w", err)
	}
	return c, nil
}
