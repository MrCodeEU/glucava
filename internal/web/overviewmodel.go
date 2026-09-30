package web

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/overview"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
)

// overviewModel is every number the Overview cards draw from, computed once
// per (range, data version) and cached. A card only picks from it, so
// enabling more cards costs nothing extra.
type overviewModel struct {
	From, To time.Time // the window actually analysed ("all" starts at the first reading)
	HasData  bool

	Cur  periodKPIs
	Prev *periodKPIs // nil unless comparing

	Acts     []jobs.Activity
	Overview overview.Data // per-activity aggregates (by sport)
	Windows  []analytics.Window

	TIRDuring, TIRRest analytics.TIR5
	AGPAll, AGPRest    analytics.AGP
	Daily              []analytics.Day
	Heat               analytics.WeekdayHour
	Parts              [4]analytics.DayPart
	Episodes           []analytics.Episode
	Insights           []analytics.ActivityInsight
	Sports             []analytics.SportStats
}

// overviewInput is what buildOverviewModel needs; loading it is the
// server's job, so the model itself is a pure function of the data.
type overviewInput struct {
	From, To         time.Time
	Samples          []stats.Sample
	Acts             []jobs.Activity
	PrevFrom, PrevTo time.Time
	PrevSamples      []stats.Sample // nil unless comparing
	Compare          bool
	Thr              analytics.Thresholds
	Loc              *time.Location
}

// buildOverviewModel computes the model. Samples must be ascending, as
// store.LoadSamplesFast returns them.
func buildOverviewModel(in overviewInput) *overviewModel {
	m := &overviewModel{From: in.From, To: in.To, Acts: in.Acts}
	m.Overview = overview.Build(in.Acts, in.Loc)
	for _, a := range in.Acts {
		if a.Duration > 0 {
			m.Windows = append(m.Windows, analytics.Window{Start: a.Start, End: a.End()})
		}
	}
	m.Cur = computeKPIs(in.Samples, in.From, in.To, in.Thr, in.Loc)
	m.HasData = m.Cur.HasData
	if in.Compare {
		p := computeKPIs(in.PrevSamples, in.PrevFrom, in.PrevTo, in.Thr, in.Loc)
		m.Prev = &p
	}
	if !m.HasData {
		return m
	}
	rest := analytics.ExcludeWindows(in.Samples, m.Windows)
	m.TIRDuring = analytics.ComputeTIR5(analytics.OnlyWindows(in.Samples, m.Windows), in.Thr)
	m.TIRRest = analytics.ComputeTIR5(rest, in.Thr)
	m.AGPAll = analytics.ComputeAGP(in.Samples, in.Loc, 0)
	m.AGPRest = analytics.ComputeAGP(rest, in.Loc, 0)
	m.Daily = analytics.ComputeDaily(in.Samples, in.Thr, in.Loc)
	m.Heat = analytics.ComputeWeekdayHour(in.Samples, in.Thr, in.Loc)
	m.Parts = analytics.ComputeDayParts(in.Samples, in.Thr, in.Loc)
	m.Episodes = analytics.DetectEpisodes(in.Samples, in.Thr, analytics.EpisodeOptions{Loc: in.Loc})
	m.Insights = analytics.InsightsFor(in.Samples, activityInputs(in.Acts), in.Thr, in.Loc)
	m.Sports = analytics.BySport(m.Insights)
	return m
}

// activityInputs maps stored activities onto the analytics input type.
// Averages of heart rate are left out: the light activity loader skips the
// heart-rate series on purpose.
func activityInputs(acts []jobs.Activity) []analytics.ActivityInput {
	out := make([]analytics.ActivityInput, 0, len(acts))
	for _, a := range acts {
		if a.Duration <= 0 || a.Status != jobs.StatusDone {
			continue
		}
		out = append(out, analytics.ActivityInput{
			ID: a.StravaID, Sport: a.Sport, Start: a.Start, End: a.End(),
			DistanceM: a.Distance, ElevationGain: a.ElevationGain,
		})
	}
	return out
}

// overviewKey identifies a computed model. Rolling ranges move their end
// every second, so the window is bucketed to five minutes; a new reading or
// an edited activity changes Ver and so misses regardless.
type overviewKey struct {
	Range    string
	From, To int64
	Compare  bool
	Loc      string
	Thr      analytics.Thresholds
	Ver      store.DataVersion
}

const overviewCacheMax = 16

// overviewCache is a small in-memory memo of overview models. The zero
// value is ready to use.
type overviewCache struct {
	mu sync.Mutex
	m  map[overviewKey]*overviewModel
}

func (c *overviewCache) get(k overviewKey) (*overviewModel, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.m[k]
	return m, ok
}

func (c *overviewCache) put(k overviewKey, m *overviewModel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) >= overviewCacheMax {
		c.m = map[overviewKey]*overviewModel{} // old versions are dead weight: start over
	}
	c.m[k] = m
}

// overviewModelFor loads the range's data and returns its (possibly cached) model.
func (s *Server) overviewModelFor(ctx context.Context, r statsRange, cfg store.Config) (*overviewModel, error) {
	loc := s.loc()
	thr := analytics.FromRange(cfg.Range())
	ver, err := s.Store.DataVersion(ctx)
	if err != nil {
		return nil, err
	}
	key := overviewKey{
		Range: r.Key, From: r.From.Unix() / 300, To: r.To.Unix() / 300, Compare: r.Compare,
		Loc: loc.String(), Thr: thr, Ver: ver,
	}
	if m, ok := s.overviewCache.get(key); ok {
		return m, nil
	}
	in := overviewInput{From: r.From, To: r.To, Compare: r.Compare, PrevFrom: r.PrevFrom, PrevTo: r.PrevTo, Thr: thr, Loc: loc}
	if in.Samples, err = s.Store.LoadSamplesFast(ctx, r.From, r.To); err != nil {
		return nil, err
	}
	if in.Acts, err = s.Store.ActivitiesInRangeLight(ctx, r.From, r.To); err != nil {
		return nil, err
	}
	if r.Compare {
		if in.PrevSamples, err = s.Store.LoadSamplesFast(ctx, r.PrevFrom, r.PrevTo); err != nil {
			return nil, err
		}
	}
	if r.Key == "all" {
		in.From = earliest(in.Samples, in.Acts, r.To, loc)
	}
	m := buildOverviewModel(in)
	s.overviewCache.put(key, m)
	return m, nil
}

// earliest is the local midnight of the first reading or activity, or to
// when there is none: the start of an "all time" window.
func earliest(samples []stats.Sample, acts []jobs.Activity, to time.Time, loc *time.Location) time.Time {
	first := to
	if len(samples) > 0 && samples[0].Time.Before(first) {
		first = samples[0].Time
	}
	for _, a := range acts {
		if a.Start.Before(first) {
			first = a.Start
		}
	}
	y, mo, d := first.In(loc).Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, loc)
}

// recentEpisodes returns up to n of the low and high episodes, newest first.
// Very-low and very-high runs are nested inside the wider low and high ones,
// so only those two kinds are listed and the severity comes from the extreme.
func recentEpisodes(eps []analytics.Episode, n int) []analytics.Episode {
	var out []analytics.Episode
	for _, e := range eps {
		if e.Kind == analytics.KindLow || e.Kind == analytics.KindHigh {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// nearbyActivity finds the activity an episode happened during or within
// four hours after, the window in which exercise-related lows show up.
func nearbyActivity(acts []jobs.Activity, e analytics.Episode) (jobs.Activity, bool) {
	var best jobs.Activity
	found := false
	for _, a := range acts {
		if e.Start.Before(a.Start) || e.Start.After(a.End().Add(4*time.Hour)) {
			continue
		}
		if !found || a.Start.After(best.Start) {
			best, found = a, true
		}
	}
	return best, found
}
