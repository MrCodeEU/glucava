package web

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/starfederation/datastar-go/datastar"

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
	flag("hr", &cfg.ChartHR)
	if v, ok := get("pre"); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 240 {
			cfg.ChartPreMin = n
		}
	}
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
	pre, post := time.Duration(max(cfg.PreMin, cfg.ChartPreMin))*time.Minute, time.Duration(cfg.PostMin)*time.Minute

	var (
		samples    []stats.Sample
		hr         []chartimg.HRPoint
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
		samples, start, end, hr = got, act.Start, act.End(), act.HeartRate
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
			hr = sampleHR(start)
		}
	} else if !load(name) {
		http.NotFound(w, r)
		return
	}

	png, err := chartimg.Photo(chartimg.PhotoData{
		Samples: samples, HR: hr, Range: stats.Range{Low: cfg.RangeLow, High: cfg.RangeHigh},
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

// descriptionPreview renders the "tmpl" query parameter (a candidate
// description template) against the same real-latest-activity-or-sample-data
// used by the chart preview, and patches the descPreview signal with the
// result. The settings page calls this as someone edits a template, so they
// see exactly what would be written to Strava before saving anything. A
// template that fails to parse or execute patches an error message instead,
// never silently.
func (s *Server) descriptionPreview(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		s.serverError(w, err)
		return
	}
	cfg = chartConfig(cfg, r.URL.Query())
	tmplText := r.URL.Query().Get("tmpl")
	text := s.previewDescriptionText(r.Context(), tmplText, cfg)
	patch, _ := json.Marshal(map[string]string{"descPreview": text})
	_ = sse.PatchSignals(patch)
}

// previewDescriptionText renders tmplText (or render.DefaultTemplate, if
// empty) against the newest activity with at least two readings, or
// made-up sample data if there is none yet. Shared by descriptionPreview
// (live, as someone edits the template) and settingsPage (the value shown
// before any edit).
func (s *Server) previewDescriptionText(ctx context.Context, tmplText string, cfg store.Config) string {
	if tmplText == "" {
		tmplText = render.DefaultTemplate
	}
	var samples []stats.Sample
	var start, end time.Time
	acts, _ := s.Store.ListActivities(ctx, 30)
	for _, a := range acts {
		got, err := s.Store.LoadSamplesAny(ctx, a.Start, a.End())
		if err == nil && len(got) >= 2 {
			samples, start, end = got, a.Start, a.End()
			break
		}
	}
	if samples == nil {
		samples, start, end = sampleCurve(s.now())
	}

	var inWindow []stats.Sample
	for _, sp := range samples {
		if !sp.Time.Before(start) && !sp.Time.After(end) {
			inWindow = append(inWindow, sp)
		}
	}
	if len(inWindow) == 0 {
		inWindow = samples
	}
	sum, ok := stats.Summarize(inWindow, stats.Range{Low: cfg.RangeLow, High: cfg.RangeHigh})
	if !ok {
		return "⚠ No sample data to preview with."
	}
	block, rerr := render.RenderBlock(tmplText, sum, inWindow, render.Options{Unit: render.Unit(cfg.Unit)})
	if rerr != nil {
		return "⚠ Template error: " + rerr.Error()
	}
	return block
}

// sampleCurve is a made-up run for previews when no real readings exist yet.
func sampleCurve(now time.Time) (samples []stats.Sample, start, end time.Time) {
	start = now.Truncate(time.Hour).Add(-3 * time.Hour)
	for i := -9; i < 30; i++ {
		v := 115 + 55*math.Sin(float64(i)/6) + 25*math.Sin(float64(i)/2.3)
		samples = append(samples, stats.Sample{Time: start.Add(time.Duration(i) * 5 * time.Minute), Value: v})
	}
	return samples, start, start.Add(100 * time.Minute)
}

// sampleHR is a made-up heart rate for the sample run.
func sampleHR(start time.Time) []chartimg.HRPoint {
	var hr []chartimg.HRPoint
	for i := 0; i < 100; i += 2 {
		hr = append(hr, chartimg.HRPoint{Time: start.Add(time.Duration(i) * time.Minute), BPM: 135 + 18*math.Sin(float64(i)/9) + float64(i)/8})
	}
	return hr
}
