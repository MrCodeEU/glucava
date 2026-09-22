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
		b, err := w.fetchActivitiesJSON(ctx, limit, 1)
		if err != nil {
			return err
		}
		body = b
		return w.exportCookies(ctx)
	})
	return body, err
}

// maxFindPages bounds how many pages FindActivity will page through before
// giving up, so a bad id can't turn into an unbounded fetch loop.
const maxFindPages = 10

// findPageSize is per_page for FindActivity's paging; smaller than the
// default fetch limit so it costs less per page while still covering
// months of activity history within maxFindPages.
const findPageSize = 30

// FindActivity looks up a single activity by Strava id, paging through the
// training log (newest first) until it is found or maxFindPages is
// exhausted. Unlike ListRecent, this can find an activity the poller never
// queued, e.g. because it predates this app or is older than its MaxAge, so
// it can be processed by hand from the web UI.
func (w *Writer) FindActivity(ctx context.Context, stravaID string) (*jobs.Activity, error) {
	var found *jobs.Activity
	err := w.withBrowser(ctx, func(ctx context.Context) error {
		loc, err := w.navigate(ctx, w.cfg.BaseURL+"/dashboard")
		if err != nil {
			return err
		}
		if isLoginURL(loc) {
			return ErrSessionExpired
		}
		for page := 1; page <= maxFindPages; page++ {
			raw, err := w.fetchActivitiesJSON(ctx, findPageSize, page)
			if err != nil {
				return err
			}
			acts, err := ParseTrainingActivities(raw, w.cfg.Location)
			if err != nil {
				return err
			}
			if len(acts) == 0 {
				break // no more pages
			}
			for i := range acts {
				if acts[i].StravaID == stravaID {
					found = &acts[i]
					return w.exportCookies(ctx)
				}
			}
		}
		return w.exportCookies(ctx)
	})
	return found, err
}

// fetchActivitiesJSON fetches one page of the training log. It assumes the
// browser is already on a Strava page with a valid session (see navigate).
func (w *Writer) fetchActivitiesJSON(ctx context.Context, limit, page int) ([]byte, error) {
	js := `fetch(` + jsStr(fmt.Sprintf("%s?per_page=%d&page=%d", listPath, limit, page)) + `,{credentials:'include',redirect:'manual',
	  headers:{'X-Requested-With':'XMLHttpRequest','Accept':'application/json'}})
	  .then(r=>r.text().then(t=>JSON.stringify({type:r.type,status:r.status,body:t})))`
	var res string
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &res, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
	if err != nil {
		return nil, fmt.Errorf("strava: fetch activity list: %w", err)
	}
	var r struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(res), &r); err != nil {
		return nil, fmt.Errorf("strava: fetch activity list: %w", err)
	}
	switch {
	case r.Type == "opaqueredirect", r.Status == 401, r.Status == 403:
		return nil, ErrSessionExpired
	case r.Status != 200:
		return nil, fmt.Errorf("strava: activity list: HTTP %d", r.Status)
	}
	return []byte(r.Body), nil
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
		// The real training-log response (checked 2026-09) gives elapsed_time
		// and moving_time as formatted strings like "46:50", not numbers; the
		// numeric seconds are in the _raw variants. Both plain names are kept
		// as a fallback in case an older or different shape sends numbers directly.
		secs := firstNumber(m, "elapsed_time_raw", "moving_time_raw", "elapsed_time", "moving_time")
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
