package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/chartimg"
)

// maxElevPoints bounds what is kept per activity, same reasoning as
// maxHRPoints: Strava serves far more altitude samples than a chart needs.
const maxElevPoints = 300

// Elevation implements jobs.ElevationSource. It reads the activity's
// altitude stream from the same web endpoint HeartRate uses (checked
// 2026-09), and returns it thinned to at most maxElevPoints readings. An
// activity without an altitude stream (e.g. an indoor trainer entry)
// returns no points and no error.
func (w *Writer) Elevation(ctx context.Context, stravaID string, start time.Time) ([]chartimg.ElevPoint, error) {
	if !idRe.MatchString(stravaID) {
		return nil, fmt.Errorf("strava: invalid activity id %q", stravaID)
	}
	var pts []chartimg.ElevPoint
	err := w.withBrowser(ctx, func(ctx context.Context) error {
		loc, err := w.navigate(ctx, w.cfg.BaseURL+"/activities/"+stravaID)
		if err != nil {
			return err
		}
		if isLoginURL(loc) {
			return ErrSessionExpired
		}
		q := url.Values{}
		q.Add("stream_types[]", "altitude")
		q.Add("stream_types[]", "distance")
		q.Add("stream_types[]", "time")
		status, _, _, body, err := w.fetchRaw(ctx, "/activities/"+stravaID+"/streams?"+q.Encode())
		if err != nil {
			return err
		}
		switch {
		case status == 401 || status == 403:
			return ErrSessionExpired
		case status == 404 || status == 204:
			return nil // no streams: a manual activity, say
		case status != 200:
			return fmt.Errorf("strava: elevation stream answered %d", status)
		}
		if pts, err = ParseElevation([]byte(body), start); err != nil {
			return err
		}
		return w.exportCookies(ctx)
	})
	return pts, err
}

// ParseElevation turns the streams response ({"altitude":[...],"time":[...]},
// time in seconds since the start) into points, thinned to maxElevPoints by
// averaging, same as ParseHeartRate.
func ParseElevation(raw []byte, start time.Time) ([]chartimg.ElevPoint, error) {
	var s struct {
		Alt  []float64 `json:"altitude"`
		Time []float64 `json:"time"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("strava: elevation stream is not the expected JSON: %w", err)
	}
	n := min(len(s.Alt), len(s.Time))
	if n == 0 {
		return nil, nil
	}
	var pts []chartimg.ElevPoint
	for i := 0; i < n; i++ {
		pts = append(pts, chartimg.ElevPoint{Time: start.Add(time.Duration(s.Time[i] * float64(time.Second))), Meters: s.Alt[i]})
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].Time.Before(pts[j].Time) })
	return thinElev(pts, maxElevPoints), nil
}

// thinElev averages pts down to at most max points, in equal-sized runs.
// Same shape as thin() in heartrate.go, duplicated rather than made generic
// since chartimg.HRPoint and chartimg.ElevPoint don't share a value field
// name to abstract over cleanly.
func thinElev(pts []chartimg.ElevPoint, max int) []chartimg.ElevPoint {
	if len(pts) <= max {
		return pts
	}
	size := (len(pts) + max - 1) / max
	var out []chartimg.ElevPoint
	for i := 0; i < len(pts); i += size {
		end := min(i+size, len(pts))
		var sum float64
		var ts int64
		for _, p := range pts[i:end] {
			sum += p.Meters
			ts += p.Time.UnixNano() / int64(end-i)
		}
		out = append(out, chartimg.ElevPoint{Time: time.Unix(0, ts), Meters: sum / float64(end-i)})
	}
	return out
}
