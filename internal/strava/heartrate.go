package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/chartimg"
)

// maxHRPoints bounds what is kept per activity: Strava serves about one
// reading per second, far more than a chart or the database needs.
const maxHRPoints = 300

// HeartRate implements jobs.HRSource. It reads the activity's heart rate
// stream from the web endpoint the analysis page uses (checked 2026-09 with
// "make strava-hr"), and returns it thinned to at most maxHRPoints readings.
// An activity without heart rate returns no points and no error.
func (w *Writer) HeartRate(ctx context.Context, stravaID string, start time.Time) ([]chartimg.HRPoint, error) {
	if !idRe.MatchString(stravaID) {
		return nil, fmt.Errorf("strava: invalid activity id %q", stravaID)
	}
	var pts []chartimg.HRPoint
	err := w.withBrowser(ctx, func(ctx context.Context) error {
		loc, err := w.navigate(ctx, w.cfg.BaseURL+"/activities/"+stravaID)
		if err != nil {
			return err
		}
		if isLoginURL(loc) {
			return ErrSessionExpired
		}
		q := url.Values{}
		q.Add("stream_types[]", "heartrate")
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
			return fmt.Errorf("strava: heart rate stream answered %d", status)
		}
		if pts, err = ParseHeartRate([]byte(body), start); err != nil {
			return err
		}
		return w.exportCookies(ctx)
	})
	return pts, err
}

// ParseHeartRate turns the streams response ({"heartrate":[...],"time":[...]},
// time in seconds since the start) into points, thinned to maxHRPoints by
// averaging. Implausible readings (0, or under 30 or over 250 bpm) are dropped.
func ParseHeartRate(raw []byte, start time.Time) ([]chartimg.HRPoint, error) {
	var s struct {
		HR   []float64 `json:"heartrate"`
		Time []float64 `json:"time"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("strava: heart rate stream is not the expected JSON: %w", err)
	}
	n := min(len(s.HR), len(s.Time))
	if n == 0 {
		return nil, nil
	}
	var pts []chartimg.HRPoint
	for i := 0; i < n; i++ {
		if s.HR[i] < 30 || s.HR[i] > 250 {
			continue
		}
		pts = append(pts, chartimg.HRPoint{Time: start.Add(time.Duration(s.Time[i] * float64(time.Second))), BPM: s.HR[i]})
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].Time.Before(pts[j].Time) })
	return thin(pts, maxHRPoints), nil
}

// thin averages pts down to at most max points, in equal-sized runs.
func thin(pts []chartimg.HRPoint, max int) []chartimg.HRPoint {
	if len(pts) <= max {
		return pts
	}
	size := int(math.Ceil(float64(len(pts)) / float64(max)))
	var out []chartimg.HRPoint
	for i := 0; i < len(pts); i += size {
		end := min(i+size, len(pts))
		var sum float64
		var ts int64
		for _, p := range pts[i:end] {
			sum += p.BPM
			ts += p.Time.UnixNano() / int64(end-i)
		}
		out = append(out, chartimg.HRPoint{Time: time.Unix(0, ts), BPM: math.Round(sum/float64(end-i)*10) / 10})
	}
	return out
}
