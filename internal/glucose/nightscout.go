package glucose

import (
	"context"
	"crypto/sha1" //nolint:gosec // Nightscout's API-SECRET header is the SHA-1 of the secret; it is the protocol, not a choice
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/stats"
)

func init() {
	Register(Descriptor{
		ID:         "nightscout",
		LabelKey:   i18n.Key("source.nightscout.label"),
		IntroKey:   i18n.Key("source.nightscout.intro"),
		Unverified: true,
		Fields: []Field{
			{Key: "url", Kind: KindURL, LabelKey: i18n.Key("source.nightscout.url.label"), HelpKey: i18n.Key("source.nightscout.url.help"), Required: true},
			{Key: "token", Kind: KindPassword, LabelKey: i18n.Key("source.nightscout.token.label"), HelpKey: i18n.Key("source.nightscout.token.help")},
			{Key: "apisecret", Kind: KindPassword, LabelKey: i18n.Key("source.nightscout.apisecret.label"), HelpKey: i18n.Key("source.nightscout.apisecret.help")},
		},
		Build: func(v Values) (Source, error) {
			return &Nightscout{BaseURL: v.Get("url"), Token: v.Get("token"), APISecret: v.Get("apisecret"), Client: &http.Client{Timeout: 30 * time.Second}}, nil
		},
	})
}

// Nightscout reads sensor values from a Nightscout server's entries API
// (https://github.com/nightscout/cgm-remote-monitor). Nightscout keeps its
// history, so a window is never "too old". An access token (Admin Tools →
// Subjects) or the API secret authenticates; a server that allows anonymous
// reads needs neither.
type Nightscout struct {
	BaseURL   string
	Token     string // access token, sent as ?token=
	APISecret string // API_SECRET, sent as its SHA-1 in the api-secret header
	Client    *http.Client
}

// maxNightscoutEntries caps one request; at 5 minute spacing that is about 35 days.
const maxNightscoutEntries = 10000

type nightscoutEntry struct {
	Type string  `json:"type"`
	SGV  float64 `json:"sgv"`
	Date float64 `json:"date"` // epoch milliseconds
}

// Samples implements Source.
func (n *Nightscout) Samples(ctx context.Context, from, to time.Time) ([]stats.Sample, error) {
	u, err := url.Parse(strings.TrimRight(n.BaseURL, "/") + "/api/v1/entries/sgv.json")
	if err != nil {
		return nil, fmt.Errorf("nightscout: bad address: %w", err)
	}
	q := url.Values{
		"find[date][$gte]": {strconv.FormatInt(from.UnixMilli(), 10)},
		"find[date][$lte]": {strconv.FormatInt(to.UnixMilli(), 10)},
		"count":            {strconv.Itoa(maxNightscoutEntries)},
	}
	if n.Token != "" {
		q.Set("token", n.Token)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if n.APISecret != "" {
		sum := sha1.Sum([]byte(n.APISecret)) //nolint:gosec // see the import
		req.Header.Set("api-secret", hex.EncodeToString(sum[:]))
	}
	c := n.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, scrubURL(err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrAuth
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("nightscout: HTTP %d", resp.StatusCode)
	}
	var entries []nightscoutEntry
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 16<<20)).Decode(&entries); err != nil {
		return nil, fmt.Errorf("nightscout: decode entries: %w", err)
	}
	out := make([]stats.Sample, 0, len(entries))
	for _, e := range entries {
		if (e.Type != "" && e.Type != "sgv") || e.SGV <= 0 || e.Date <= 0 {
			continue
		}
		t := time.UnixMilli(int64(e.Date)).UTC()
		if t.Before(from) || t.After(to) {
			continue
		}
		out = append(out, stats.Sample{Time: t, Value: e.SGV})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

// scrubURL drops the request URL from a transport error: it carries the access
// token in its query string, and errors end up in logs and notifications.
func scrubURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
