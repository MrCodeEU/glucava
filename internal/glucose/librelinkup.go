package glucose

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/stats"
)

func init() {
	region := func(v, label string) Option { return Option{Value: v, LabelKey: label} }
	Register(Descriptor{
		ID:         "librelinkup",
		LabelKey:   i18n.Key("source.librelinkup.label"),
		IntroKey:   i18n.Key("source.librelinkup.intro"),
		Unverified: true,
		Fields: []Field{
			{Key: "region", Kind: KindSelect, LabelKey: i18n.Key("source.librelinkup.region.label"), HelpKey: i18n.Key("source.librelinkup.region.help"), Default: "global", Options: []Option{
				region("global", i18n.Key("source.librelinkup.region.global")),
				region("eu", i18n.Key("source.librelinkup.region.eu")), region("eu2", i18n.Key("source.librelinkup.region.eu2")),
				region("de", i18n.Key("source.librelinkup.region.de")), region("fr", i18n.Key("source.librelinkup.region.fr")),
				region("us", i18n.Key("source.librelinkup.region.us")), region("ca", i18n.Key("source.librelinkup.region.ca")),
				region("jp", i18n.Key("source.librelinkup.region.jp")), region("ap", i18n.Key("source.librelinkup.region.ap")),
				region("au", i18n.Key("source.librelinkup.region.au")), region("ae", i18n.Key("source.librelinkup.region.ae")),
				region("la", i18n.Key("source.librelinkup.region.la")), region("ru", i18n.Key("source.librelinkup.region.ru")),
			}},
			{Key: "email", Kind: KindText, LabelKey: i18n.Key("source.librelinkup.email.label"), HelpKey: i18n.Key("source.librelinkup.email.help"), Required: true},
			{Key: "password", Kind: KindPassword, LabelKey: i18n.Key("source.librelinkup.password.label"), Required: true},
		},
		Build: func(v Values) (Source, error) {
			return NewLibreLinkUp(v.Get("region"), v.Get("email"), v.Get("password")), nil
		},
	})
}

// LibreLinkUp reads glucose values through the unofficial LibreLinkUp API that
// the follower app uses. The account is a follower (an invited LibreLinkUp
// user) of the sensor wearer; the wearer's own LibreView login does not work.
//
// UNVERIFIED: written from the API as other open-source clients describe it,
// never run against a real account. The wire format below is where to look
// first when it fails.
type LibreLinkUp struct {
	Region   string // "global", "eu", "de", ... (see the descriptor)
	Email    string
	Password string
	Client   *http.Client
	Now      func() time.Time // for tests

	// BaseURL overrides the region host (tests). A login reply that names
	// another region replaces it for the following calls.
	BaseURL string

	mu        sync.Mutex
	base      string
	token     string
	accountID string // sha256 of the user id, sent as Account-Id
	patientID string
}

// NewLibreLinkUp returns a client for the given region.
func NewLibreLinkUp(region, email, password string) *LibreLinkUp {
	return &LibreLinkUp{Region: region, Email: email, Password: password, Client: &http.Client{Timeout: 30 * time.Second}}
}

// libreKeep is how far back the follower graph reaches (about 12 hours).
const libreKeep = 12 * time.Hour

// libreAppVersion is sent as the "version" header. LibreLinkUp refuses
// versions it considers too old, so keep this near the current Android app.
const libreAppVersion = "4.16.0"

func (l *LibreLinkUp) regionBase(region string) string {
	if region == "" || region == "global" {
		return "https://api.libreview.io"
	}
	return "https://api-" + region + ".libreview.io"
}

type libreEnvelope struct {
	Status int             `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
}

type libreReading struct {
	Value     float64 `json:"ValueInMgPerDl"`
	Timestamp string  `json:"FactoryTimestamp"` // UTC, "1/2/2006 3:04:05 PM"
}

// Samples implements Source. The follower graph holds about 12 hours, so an
// older window returns ErrTooOld.
func (l *LibreLinkUp) Samples(ctx context.Context, from, to time.Time) ([]stats.Sample, error) {
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	if now.Sub(from) > libreKeep {
		return nil, ErrTooOld
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if l.token == "" {
			if err := l.login(ctx); err != nil {
				return nil, err
			}
		}
		out, err := l.graph(ctx, from, to)
		if errors.Is(err, errLibreSession) && attempt == 0 {
			l.token = ""
			continue
		}
		return out, err
	}
}

var errLibreSession = errors.New("librelinkup: session expired")

func (l *LibreLinkUp) login(ctx context.Context) error {
	l.base = l.BaseURL
	if l.base == "" {
		l.base = l.regionBase(l.Region)
	}
	for hop := 0; hop < 3; hop++ { // a login may point at another region once
		body, _ := json.Marshal(map[string]string{"email": l.Email, "password": l.Password})
		env, status, err := l.call(ctx, http.MethodPost, "/llu/auth/login", body, false)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized || env.Status == 2 {
			return ErrAuth
		}
		if status != http.StatusOK {
			return fmt.Errorf("librelinkup: login: HTTP %d: %s", status, env.Error.Message)
		}
		var data struct {
			Redirect bool   `json:"redirect"`
			Region   string `json:"region"`
			Step     *struct {
				Type string `json:"type"`
			} `json:"step"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			AuthTicket struct {
				Token string `json:"token"`
			} `json:"authTicket"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return fmt.Errorf("librelinkup: decode login reply: %w", err)
		}
		if data.Redirect && data.Region != "" {
			if l.BaseURL == "" {
				l.base = l.regionBase(data.Region)
			}
			continue
		}
		if data.AuthTicket.Token == "" {
			if data.Step != nil {
				return fmt.Errorf("librelinkup: LibreLinkUp wants a step done in its app first (%s), accept it there and retry", data.Step.Type)
			}
			return ErrAuth
		}
		sum := sha256.Sum256([]byte(data.User.ID))
		l.token, l.accountID = data.AuthTicket.Token, hex.EncodeToString(sum[:])
		l.patientID = ""
		return nil
	}
	return errors.New("librelinkup: too many region redirects")
}

func (l *LibreLinkUp) graph(ctx context.Context, from, to time.Time) ([]stats.Sample, error) {
	if l.patientID == "" {
		env, status, err := l.call(ctx, http.MethodGet, "/llu/connections", nil, true)
		if err != nil {
			return nil, err
		}
		if status == http.StatusUnauthorized {
			return nil, errLibreSession
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("librelinkup: connections: HTTP %d", status)
		}
		var conns []struct {
			PatientID string `json:"patientId"`
		}
		if err := json.Unmarshal(env.Data, &conns); err != nil || len(conns) == 0 || conns[0].PatientID == "" {
			return nil, errors.New("librelinkup: this account follows nobody; invite it as a LibreLinkUp follower in the sensor wearer's Libre app")
		}
		l.patientID = conns[0].PatientID
	}
	env, status, err := l.call(ctx, http.MethodGet, "/llu/connections/"+l.patientID+"/graph", nil, true)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, errLibreSession
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("librelinkup: graph: HTTP %d", status)
	}
	var data struct {
		Connection struct {
			Latest *libreReading `json:"glucoseMeasurement"`
		} `json:"connection"`
		GraphData []libreReading `json:"graphData"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("librelinkup: decode graph: %w", err)
	}
	all := data.GraphData
	if data.Connection.Latest != nil {
		all = append(all, *data.Connection.Latest)
	}
	seen := map[time.Time]bool{}
	var out []stats.Sample
	for _, r := range all {
		t, err := time.ParseInLocation("1/2/2006 3:04:05 PM", r.Timestamp, time.UTC)
		if err != nil || r.Value <= 0 || t.Before(from) || t.After(to) || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, stats.Sample{Time: t, Value: r.Value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

func (l *LibreLinkUp) call(ctx context.Context, method, path string, body []byte, auth bool) (libreEnvelope, int, error) {
	var env libreEnvelope
	req, err := http.NewRequestWithContext(ctx, method, l.base+path, bytes.NewReader(body))
	if err != nil {
		return env, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("product", "llu.android")
	req.Header.Set("version", libreAppVersion)
	if auth {
		req.Header.Set("Authorization", "Bearer "+l.token)
		req.Header.Set("Account-Id", l.accountID)
	}
	c := l.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return env, 0, scrubURL(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return env, resp.StatusCode, err
	}
	// A body that is not the usual envelope still gives a usable status code.
	if jerr := json.Unmarshal(raw, &env); jerr != nil && resp.StatusCode == http.StatusOK {
		return env, resp.StatusCode, fmt.Errorf("librelinkup: unexpected reply: %s", snippet(raw))
	}
	env.Error.Message = strings.TrimSpace(env.Error.Message)
	return env, resp.StatusCode, nil
}
