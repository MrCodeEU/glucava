package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// HRAttempt is what one candidate address for heart rate data answered.
type HRAttempt struct {
	Path        string
	Status      int
	Type        string // response type: "basic", "opaqueredirect", ...
	ContentType string
	Bytes       int
	Shape       string // JSON: keys with array lengths; otherwise a short start of the body
}

// hrCandidates are guesses at where Strava's web UI gets heart rate from. None
// is documented; ProbeHeartRate checks which ones answer for the account.
func hrCandidates(id string) []string {
	q := url.Values{}
	q.Add("stream_types[]", "heartrate")
	q.Add("stream_types[]", "time")
	return []string{
		"/activities/" + id + "/streams?" + q.Encode(),
		"/stream/activities/" + id + "?" + q.Encode(),
		"/activities/" + id + "/export_tcx",
		"/activities/" + id + "/export_gpx",
	}
}

// ProbeHeartRate is a read-only dry run: it opens the activity page and asks
// each candidate address for heart rate data with the session's cookies. It
// reports what came back and whether the page itself mentions heart rate. It
// changes nothing on Strava.
func (w *Writer) ProbeHeartRate(ctx context.Context, stravaID string) ([]HRAttempt, string, error) {
	if !idRe.MatchString(stravaID) {
		return nil, "", fmt.Errorf("strava: invalid activity id %q", stravaID)
	}
	var out []HRAttempt
	var pageHint string
	err := w.withBrowser(ctx, func(ctx context.Context) error {
		loc, err := w.navigate(ctx, w.cfg.BaseURL+"/activities/"+stravaID)
		if err != nil {
			return err
		}
		if isLoginURL(loc) {
			return ErrSessionExpired
		}
		pageHint, _ = evalString(ctx, `(function(){const h=document.documentElement.outerHTML;
		  const i=h.search(/heart ?rate|heartrate|herzfrequenz/i);
		  return 'page mentions heart rate: '+(i>=0)+(i>=0?' near: '+h.slice(Math.max(0,i-80),i+120).replace(/\s+/g,' '):'')})()`)
		for _, p := range hrCandidates(stravaID) {
			a, err := w.fetchProbe(ctx, p)
			if err != nil {
				return err
			}
			out = append(out, a)
		}
		return w.exportCookies(ctx)
	})
	return out, pageHint, err
}

func (w *Writer) fetchProbe(ctx context.Context, path string) (HRAttempt, error) {
	js := `fetch(` + jsStr(path) + `,{credentials:'include',redirect:'manual',
	  headers:{'X-Requested-With':'XMLHttpRequest','Accept':'application/json, text/plain, */*'}})
	  .then(r=>r.text().then(t=>JSON.stringify({type:r.type,status:r.status,ct:r.headers.get('content-type')||'',body:t.slice(0,2000000)})))`
	var res string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &res, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})); err != nil {
		return HRAttempt{}, fmt.Errorf("strava: fetch %s: %w", path, err)
	}
	var r struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
		CT     string `json:"ct"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(res), &r); err != nil {
		return HRAttempt{}, err
	}
	return HRAttempt{Path: path, Status: r.Status, Type: r.Type, ContentType: r.CT, Bytes: len(r.Body), Shape: shapeOf(r.Body)}, nil
}

// shapeOf describes a response without printing the activity's data: for
// JSON, its keys and array lengths; otherwise the first characters.
func shapeOf(body string) string {
	var v any
	if json.Unmarshal([]byte(body), &v) == nil {
		return "json " + describe(v, 0)
	}
	s := strings.Join(strings.Fields(body), " ")
	if len(s) > 160 {
		s = s[:160]
	}
	return "text: " + s
}

func describe(v any, depth int) string {
	switch x := v.(type) {
	case map[string]any:
		if depth > 2 {
			return "{...}"
		}
		parts := make([]string, 0, len(x))
		for k, e := range x {
			parts = append(parts, k+":"+describe(e, depth+1))
		}
		return "{" + strings.Join(sortStrings(parts), ", ") + "}"
	case []any:
		if len(x) == 0 {
			return "[0]"
		}
		return fmt.Sprintf("[%d of %s]", len(x), describe(x[0], depth+1))
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	default:
		return "null"
	}
}

func sortStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}
