package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/MrCodeEU/glucava/internal/jobs"
)

// listPath is the JSON endpoint behind Strava's "My Activities" training log.
// It is unofficial and unverified; run "glucava strava list --raw" to see the
// real response and adjust ParseTrainingActivities if the field names differ.
const listPath = "/athlete/training_activities"

// ListRecent returns the newest activities from the logged-in web session.
func (w *Writer) ListRecent(ctx context.Context, limit int) ([]jobs.Activity, error) {
	raw, err := w.FetchRecentRaw(ctx, limit)
	if err != nil {
		return nil, err
	}
	return ParseTrainingActivities(raw, w.cfg.Location)
}

// FetchRecentRaw returns the unparsed JSON from the training log endpoint.
func (w *Writer) FetchRecentRaw(ctx context.Context, limit int) ([]byte, error) {
	if limit <= 0 {
		limit = 20
	}
	var body []byte
	err := w.withBrowser(ctx, func(ctx context.Context) error {
		loc, err := w.navigate(ctx, w.cfg.BaseURL+"/dashboard")
		if err != nil {
			return err
		}
		if isLoginURL(loc) {
			return ErrSessionExpired
		}

		js := `fetch(` + jsStr(fmt.Sprintf("%s?per_page=%d&page=1", listPath, limit)) + `,{credentials:'include',redirect:'manual',
		  headers:{'X-Requested-With':'XMLHttpRequest','Accept':'application/json'}})
		  .then(r=>r.text().then(t=>JSON.stringify({type:r.type,status:r.status,body:t})))`
		var res string
		err = chromedp.Run(ctx, chromedp.Evaluate(js, &res, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}))
		if err != nil {
			return fmt.Errorf("strava: fetch activity list: %w", err)
		}
		var r struct {
			Type   string `json:"type"`
			Status int    `json:"status"`
			Body   string `json:"body"`
		}
		if err := json.Unmarshal([]byte(res), &r); err != nil {
			return fmt.Errorf("strava: fetch activity list: %w", err)
		}
		switch {
		case r.Type == "opaqueredirect", r.Status == 401, r.Status == 403:
			return ErrSessionExpired
		case r.Status != 200:
			return fmt.Errorf("strava: activity list: HTTP %d", r.Status)
		}
		body = []byte(r.Body)
		return w.exportCookies(ctx)
	})
	return body, err
}

// ParseTrainingActivities reads the training log JSON. Times given without a zone
// (Strava's "local raw" epoch) are interpreted in loc, which defaults to time.Local.
func ParseTrainingActivities(raw []byte, loc *time.Location) ([]jobs.Activity, error) {
	if loc == nil {
		loc = time.Local
	}
	var doc struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("strava: decode activity list: %w", err)
	}

	var out []jobs.Activity
	for _, m := range doc.Models {
		id := idString(m["id"])
		if id == "" {
			continue
		}
		start, ok := activityStart(m, loc)
		if !ok {
			continue
		}
		secs := firstNumber(m, "elapsed_time", "moving_time")
		if secs <= 0 {
			continue
		}
		out = append(out, jobs.Activity{
			StravaID: id,
			Name:     str(m["name"]),
			Sport:    firstString(m, "sport_type", "type", "activity_type_display_name"),
			Start:    start,
			Duration: time.Duration(secs) * time.Second,
			Status:   jobs.StatusPending,
		})
	}
	return out, nil
}

// activityStart prefers an absolute time and falls back to the local-raw epoch.
func activityStart(m map[string]any, loc *time.Location) (time.Time, bool) {
	for _, k := range []string{"start_time", "start_date"} {
		if s, ok := m[k].(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t.UTC(), true
			}
		}
	}
	if n, ok := number(m["start_date_local_raw"]); ok {
		// The epoch encodes the wall clock as if it were UTC.
		w := time.Unix(int64(n), 0).UTC()
		return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, loc).UTC(), true
	}
	return time.Time{}, false
}

// idString returns the activity id as digits, or "" for anything else. The id ends
// up in URLs and page scripts, so only plain numbers are accepted.
func idString(v any) string {
	switch t := v.(type) {
	case string:
		if idRe.MatchString(t) {
			return t
		}
	case float64:
		if t == math.Trunc(t) && t > 0 {
			return strconv.FormatInt(int64(t), 10)
		}
	}
	return ""
}

func number(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func firstNumber(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if n, ok := number(m[k]); ok && n > 0 {
			return n
		}
	}
	return 0
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m[k]); s != "" {
			return s
		}
	}
	return ""
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
