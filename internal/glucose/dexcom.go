package glucose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// Dexcom Share region base URLs.
const (
	RegionUS  = "https://share2.dexcom.com/ShareWebServices/Services"
	RegionOUS = "https://shareous1.dexcom.com/ShareWebServices/Services"
	RegionJP  = "https://share.dexcom.jp/ShareWebServices/Services"
)

const (
	// dexcomAppID is the application id the official Dexcom follower app sends.
	dexcomAppID = "d89443d2-327c-4a6f-89e5-496bbb0317db"
	nullUUID    = "00000000-0000-0000-0000-000000000000"
	// Share keeps only the last 24 hours (288 readings at 5 minute spacing).
	maxMinutes = 1440
	maxCount   = 288
)

// DexcomShare reads glucose values through the unofficial Dexcom Share API.
// The account must have Dexcom Share enabled in the Dexcom app.
type DexcomShare struct {
	BaseURL  string // one of the Region constants
	Username string // account name, e-mail or phone number
	Password string
	Client   *http.Client
	Now      func() time.Time // for tests; defaults to time.Now

	mu      sync.Mutex
	session string
}

// NewDexcomShare returns a client for the given region base URL.
func NewDexcomShare(baseURL, username, password string) *DexcomShare {
	return &DexcomShare{
		BaseURL:  baseURL,
		Username: username,
		Password: password,
		Client:   &http.Client{Timeout: 30 * time.Second},
	}
}

var dateRe = regexp.MustCompile(`\d+`)

type reading struct {
	ST    string  `json:"ST"`
	WT    string  `json:"WT"`
	Value float64 `json:"Value"`
}

func (r reading) time() (time.Time, bool) {
	for _, s := range []string{r.ST, r.WT} {
		if m := dateRe.FindString(s); m != "" {
			if ms, err := strconv.ParseInt(m, 10, 64); err == nil {
				return time.UnixMilli(ms).UTC(), true
			}
		}
	}
	return time.Time{}, false
}

// Samples implements Source. Share serves only the last 24 hours, so a window
// starting earlier than that returns ErrTooOld.
func (d *DexcomShare) Samples(ctx context.Context, from, to time.Time) ([]stats.Sample, error) {
	now := time.Now()
	if d.Now != nil {
		now = d.Now()
	}
	// Add a minute of slack: the server counts minutes back from its own clock.
	minutes := int(now.Sub(from).Minutes()) + 1
	if minutes > maxMinutes {
		return nil, ErrTooOld
	}
	if minutes < 1 {
		minutes = 1
	}

	body, err := d.readings(ctx, minutes)
	if err != nil {
		return nil, err
	}
	var raw []reading
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("dexcom: decode readings: %w", err)
	}

	var out []stats.Sample
	for _, r := range raw {
		t, ok := r.time()
		if !ok || t.Before(from) || t.After(to) {
			continue
		}
		out = append(out, stats.Sample{Time: t, Value: r.Value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

// readings fetches raw readings and logs in again once if the session expired.
func (d *DexcomShare) readings(ctx context.Context, minutes int) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		sid, err := d.sessionID(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		q := url.Values{
			"sessionId": {sid},
			"minutes":   {strconv.Itoa(minutes)},
			"maxCount":  {strconv.Itoa(maxCount)},
		}
		body, status, err := d.post(ctx, "/Publisher/ReadPublisherLatestGlucoseValues?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if status == http.StatusOK {
			return body, nil
		}
		if attempt == 0 && sessionExpired(body) {
			continue
		}
		return nil, fmt.Errorf("dexcom: read readings: HTTP %d: %s", status, snippet(body))
	}
}

func sessionExpired(body []byte) bool {
	s := string(body)
	return strings.Contains(s, "SessionIdNotFound") || strings.Contains(s, "SessionNotValid")
}

// sessionID returns the cached session or logs in. refresh forces a new login.
func (d *DexcomShare) sessionID(ctx context.Context, refresh bool) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.session != "" && !refresh {
		return d.session, nil
	}
	d.session = ""

	acct, err := d.loginStep(ctx, "/General/AuthenticatePublisherAccount", map[string]string{
		"accountName":   d.Username,
		"applicationId": dexcomAppID,
		"password":      d.Password,
	})
	if err != nil {
		return "", err
	}
	sid, err := d.loginStep(ctx, "/General/LoginPublisherAccountById", map[string]string{
		"accountId":     acct,
		"applicationId": dexcomAppID,
		"password":      d.Password,
	})
	if err != nil {
		return "", err
	}
	d.session = sid
	return sid, nil
}

// loginStep posts credentials and returns the quoted-string id from the reply.
func (d *DexcomShare) loginStep(ctx context.Context, path string, payload map[string]string) (string, error) {
	b, _ := json.Marshal(payload)
	body, status, err := d.post(ctx, path, b)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		if strings.Contains(string(body), "AccountPasswordInvalid") || status == http.StatusUnauthorized {
			return "", ErrAuth
		}
		return "", fmt.Errorf("dexcom: login: HTTP %d: %s", status, snippet(body))
	}
	var id string
	if err := json.Unmarshal(body, &id); err != nil {
		return "", fmt.Errorf("dexcom: decode login reply: %w", err)
	}
	if id == "" || id == nullUUID {
		return "", ErrAuth
	}
	return id, nil
}

func (d *DexcomShare) post(ctx context.Context, path string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Dexcom Share/3.0.2.11 CFNetwork/711.2.23 Darwin/14.0.0")
	c := d.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return out, resp.StatusCode, err
}

// snippet shortens an error body for messages. Bodies never contain credentials.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
