package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/report"
	"github.com/MrCodeEU/glucava/internal/store"
)

// reportConcurrency caps simultaneous Typst runs. Each takes a few hundred
// milliseconds and about 35 MB, so two at a time is plenty for a personal
// server and keeps a burst of downloads from piling up processes.
const reportConcurrency = 2

// reportCacheEntries is how many rendered PDFs are kept.
const reportCacheEntries = 4

// reportKey identifies a rendered report: the window (bucketed to five
// minutes, as for the Overview cache), unit, zone and thresholds, and the data
// version, so a new reading or an edited activity misses.
type reportKey struct {
	Range    string
	From, To int64
	Compare  bool
	Unit     string
	Loc      string
	Thr      analytics.Thresholds
	Mode     string // suspected-artifact handling, which changes the numbers
	Ver      store.DataVersion
}

// reportState is the per-Server report machinery, created on first use.
type reportState struct {
	once  sync.Once
	sem   chan struct{}
	cache *analytics.Cache[reportKey, renderedReport]
}

// renderedReport is a cached PDF with its download name.
type renderedReport struct {
	Name string
	PDF  []byte
}

func (s *Server) reports() *reportState {
	s.reportSt.once.Do(func() {
		s.reportSt.sem = make(chan struct{}, reportConcurrency)
		s.reportSt.cache = analytics.NewCache[reportKey, renderedReport](reportCacheEntries)
	})
	return &s.reportSt
}

// reportHref links to the PDF for the same window the Overview shows.
func (r statsRange) reportHref() string {
	q := url.Values{}
	if r.Key == "custom" {
		q.Set("from", r.FromStr)
		q.Set("to", r.ToStr)
	} else {
		q.Set("range", r.Key)
	}
	if r.Compare && r.Key != "all" {
		q.Set("compare", "prev")
	}
	return "/export/report.pdf?" + q.Encode()
}

// exportReport streams the glucose report for ?range= (or ?from=&to=) as a PDF
// download. It needs the Typst binary; without it the answer is a 503 that
// says so, and the rest of the UI is unaffected.
func (s *Server) exportReport(w http.ResponseWriter, r *http.Request) {
	typst, err := report.FindTypst()
	if err != nil {
		slog.Warn("report: typst unavailable", "err", err)
		http.Error(w, "The report needs the typst binary, which is not installed on this server. "+
			"The official glucava image includes it; for other setups install typst or set "+report.EnvTypst+".", http.StatusServiceUnavailable)
		return
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		s.serverError(w, err)
		return
	}
	ctx, loc, now := r.Context(), s.loc(), s.now()
	rng := parseStatsRange(r.URL.Query(), cfg.OverviewRange(), now, loc)
	thr := analytics.FromRange(cfg.Range())
	ver, err := s.Store.DataVersion(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	key := reportKey{Range: rng.Key, From: rng.From.Unix() / 300, To: rng.To.Unix() / 300, Compare: rng.Compare,
		Unit: cfg.Unit, Loc: loc.String(), Thr: thr, Mode: cfg.ArtifactsMode(), Ver: ver}

	rs := s.reports()
	if hit, ok := rs.cache.Get(key); ok {
		s.servePDF(w, hit.Name, hit.PDF)
		return
	}

	select {
	case rs.sem <- struct{}{}:
		defer func() { <-rs.sem }()
	case <-ctx.Done():
		return
	}

	in, err := s.reportInput(ctx, rng, cfg, thr, now)
	if err != nil {
		s.serverError(w, err)
		return
	}
	m := report.Build(in)
	pdf, err := m.PDF(ctx, typst)
	if err != nil {
		if errors.Is(err, report.ErrNoTypst) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		s.serverError(w, err)
		return
	}
	name := report.Filename(m.From, m.To, loc)
	rs.cache.Put(key, renderedReport{Name: name, PDF: pdf})
	s.servePDF(w, name, pdf)
}

func (s *Server) servePDF(w http.ResponseWriter, name string, pdf []byte) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pdf)
}

// reportInput loads what report.Build needs, with the fast loaders.
func (s *Server) reportInput(ctx context.Context, rng statsRange, cfg store.Config, thr analytics.Thresholds, now time.Time) (report.Input, error) {
	in := report.Input{
		From: rng.From, To: rng.To, AllTime: rng.Key == "all",
		Thr: thr, Unit: render.Unit(cfg.Unit), Loc: s.loc(), Now: now, Build: s.Build,
		Compare: rng.Compare, PrevFrom: rng.PrevFrom, PrevTo: rng.PrevTo,
	}
	var err error
	if in.Samples, err = s.Store.LoadSamplesFast(ctx, rng.From, rng.To); err != nil {
		return in, err
	}
	if in.Acts, err = s.Store.ActivitiesInRangeLight(ctx, rng.From, rng.To); err != nil {
		return in, err
	}
	hr, err := s.Store.ActivityHRStats(ctx, rng.From, rng.To)
	if err != nil {
		return in, err
	}
	in.HR = make(map[string]report.HRStat, len(hr))
	for id, h := range hr {
		in.HR[id] = report.HRStat{Avg: h.Avg, Max: h.Max}
	}
	if rng.Compare {
		if in.PrevSamples, err = s.Store.LoadSamplesFast(ctx, rng.PrevFrom, rng.PrevTo); err != nil {
			return in, err
		}
	}
	// With "exclude suspected artifacts" on, the report follows the Overview
	// and uses the same cleaned readings.
	if cfg.ArtifactsMode() == store.ArtifactExclude {
		marksFrom := rng.From
		if rng.Compare {
			marksFrom = rng.PrevFrom
		}
		stored, err := s.Store.ArtifactMarks(ctx, marksFrom, rng.To)
		if err != nil {
			return in, err
		}
		oi := overviewInput{Acts: in.Acts, Thr: thr, Loc: in.Loc, Marks: toAnalyticsMarks(stored), ArtifactMode: store.ArtifactExclude}
		in.Samples, _, _ = oi.prepare(in.Samples)
		if in.PrevSamples != nil {
			in.PrevSamples, _, _ = oi.prepare(in.PrevSamples)
		}
	}
	src, err := s.Store.SourceHealth(ctx)
	if err != nil {
		return in, err
	}
	for _, si := range src {
		in.Sources = append(in.Sources, report.Source{Name: si.Source, Count: si.Count, Latest: si.Latest})
	}
	return in, nil
}
