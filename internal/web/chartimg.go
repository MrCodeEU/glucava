package web

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/chartimg"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
)

// chartConfig overlays the chart-related query parameters on the saved
// settings. The settings page uses this to preview unsaved changes.
func chartConfig(cfg store.Config, q map[string][]string) store.Config {
	get := func(k string) (string, bool) {
		if v, ok := q[k]; ok && len(v) > 0 {
			return v[0], true
		}
		return "", false
	}
	flag := func(k string, dst *bool) {
		if v, ok := get(k); ok {
			*dst = v == "true" || v == "1"
		}
	}
	if v, ok := get("theme"); ok && (v == "light" || v == "dark") {
		cfg.ChartTheme = v
	}
	if v, ok := get("size"); ok && (v == "standard" || v == "large") {
		cfg.ChartSize = v
	}
	flag("band", &cfg.ChartBand)
	flag("activity", &cfg.ChartActivity)
	flag("dots", &cfg.ChartDots)
	if v, ok := get("line"); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 4 {
			cfg.ChartLine = n
		}
	}
	if v, ok := get("unit"); ok && (v == "mg/dL" || v == "mmol/L") {
		cfg.Unit = v
	}
	num := func(k string, dst *float64) {
		if v, ok := get(k); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 40 && f <= 400 {
				*dst = f
			}
		}
	}
	num("low", &cfg.RangeLow)
	num("high", &cfg.RangeHigh)
	if cfg.RangeHigh <= cfg.RangeLow {
		cfg.RangeLow, cfg.RangeHigh = stats.DefaultRange.Low, stats.DefaultRange.High
	}
	return cfg
}

// chartImage serves the glucose chart photo as glucava draws it for Strava.
// "/chart/<activity id>.png" draws one activity; "/chart/latest.png" draws the
// newest activity that has readings, or made-up readings if there are none.
// Query parameters override the saved chart settings for live previews.
func (s *Server) chartImage(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(r.PathValue("name"), ".png")
	if name == r.PathValue("name") || (name != "latest" && !digits.MatchString(name)) {
		http.NotFound(w, r)
		return
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		s.serverError(w, err)
		return
	}
	cfg = chartConfig(cfg, r.URL.Query())
	pre, post := time.Duration(cfg.PreMin)*time.Minute, time.Duration(cfg.PostMin)*time.Minute

	var (
		samples    []stats.Sample
		start, end time.Time
	)
	load := func(id string) bool {
		act, err := s.Store.Activity(r.Context(), id)
		if err != nil || act == nil {
			return false
		}
		got, err := s.Store.LoadSamplesAny(r.Context(), act.Start.Add(-pre), act.End().Add(post))
		if err != nil || len(got) < 2 {
			return false
		}
		samples, start, end = got, act.Start, act.End()
		return true
	}
	if name == "latest" {
		acts, _ := s.Store.ListActivities(r.Context(), 30)
		for _, a := range acts {
			if load(a.StravaID) {
				break
			}
		}
		if samples == nil {
			samples, start, end = sampleCurve(s.now())
		}
	} else if !load(name) {
		http.NotFound(w, r)
		return
	}

	png, err := chartimg.Glucose(chartimg.Series{
		Samples: samples, Range: stats.Range{Low: cfg.RangeLow, High: cfg.RangeHigh},
		Start: start, End: end, Unit: render.Unit(cfg.Unit), Loc: s.loc(), Style: cfg.ChartStyle(),
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// sampleCurve is a made-up run for previews when no real readings exist yet.
func sampleCurve(now time.Time) (samples []stats.Sample, start, end time.Time) {
	start = now.Truncate(time.Hour).Add(-3 * time.Hour)
	for i := -3; i < 30; i++ {
		v := 115 + 55*math.Sin(float64(i)/6) + 25*math.Sin(float64(i)/2.3)
		samples = append(samples, stats.Sample{Time: start.Add(time.Duration(i) * 5 * time.Minute), Value: v})
	}
	return samples, start, start.Add(100 * time.Minute)
}
