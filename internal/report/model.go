// Package report builds the printable glucose report: a numeric model from
// the stored readings and activities, deterministic SVG charts, and a PDF
// typeset by the Typst command line (see Render).
//
// The package deliberately does not import internal/web. It takes plain data
// (Input) and returns a PDF, so the web handler only loads data and streams
// the result.
package report

import (
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// HRStat is the average and peak heart rate of one activity.
type HRStat struct{ Avg, Max float64 }

// Source is one glucose source with its reading count and newest reading.
type Source struct {
	Name   string
	Count  int64
	Latest time.Time
}

// Input is everything a report is computed from. Samples must be ascending
// by time (store.LoadSamplesFast returns them that way).
type Input struct {
	From, To time.Time
	// AllTime starts the window at the first reading or activity instead of
	// From, like the Overview's "all" range.
	AllTime bool

	Samples []stats.Sample
	Acts    []jobs.Activity
	HR      map[string]HRStat // by Strava ID; nil when unknown
	Sources []Source

	Thr  analytics.Thresholds
	Unit render.Unit
	Loc  *time.Location
	Now  time.Time
	// Build is the glucava version shown in the footer.
	Build string
	// T translates every text of the report and formats its dates and
	// numbers. Nil means English.
	T *i18n.Translator

	// Compare adds the previous period's numbers as deltas on the key tiles.
	Compare          bool
	PrevFrom, PrevTo time.Time
	PrevSamples      []stats.Sample
}

// Model is the computed report, in mg/dL and raw percentages; formatting for
// the chosen unit happens when it is turned into template data.
type Model struct {
	In       Input
	From, To time.Time // the window actually analysed
	HasData  bool

	KPIs     analytics.KPIs
	Prev     *analytics.KPIs
	Delta    analytics.KPIDelta
	TIR      analytics.TIR5
	Coverage analytics.Coverage
	AGP      analytics.AGP
	Daily    []analytics.Day
	Parts    [4]analytics.DayPart
	Episodes []analytics.Episode
	EpStats  map[analytics.EpisodeKind]analytics.EpisodeStats

	Acts     []jobs.Activity // completed activities, newest first
	Insights []analytics.ActivityInsight
	Sports   []analytics.SportStats
}

// Build computes the model. It is a pure function of in.
func Build(in Input) *Model {
	if in.Loc == nil {
		in.Loc = time.UTC
	}
	m := &Model{In: in, From: in.From, To: in.To, EpStats: map[analytics.EpisodeKind]analytics.EpisodeStats{}}
	if in.AllTime {
		m.From = earliest(in.Samples, in.Acts, in.To, in.Loc)
	}
	m.Coverage = analytics.ComputeCoverage(in.Samples, m.From, m.To, 0, 0)
	if len(in.Samples) > 0 {
		m.HasData = true
		m.KPIs = analytics.ComputeKPIs(in.Samples, in.Thr, m.From, m.To, in.Loc)
		m.TIR = analytics.ComputeTIR5(in.Samples, in.Thr)
		m.AGP = analytics.ComputeAGP(in.Samples, in.Loc, 0)
		m.Daily = analytics.ComputeDaily(in.Samples, in.Thr, in.Loc)
		m.Parts = analytics.ComputeDayParts(in.Samples, in.Thr, in.Loc)
		m.Episodes = analytics.DetectEpisodes(in.Samples, in.Thr, analytics.EpisodeOptions{Loc: in.Loc})
		for _, k := range []analytics.EpisodeKind{analytics.KindVeryLow, analytics.KindLow, analytics.KindHigh, analytics.KindVeryHigh} {
			m.EpStats[k] = analytics.SummarizeEpisodes(analytics.OfKind(m.Episodes, k))
		}
	}
	if in.Compare {
		p := analytics.ComputeKPIs(in.PrevSamples, in.Thr, in.PrevFrom, in.PrevTo, in.Loc)
		m.Prev = &p
		m.Delta = analytics.DeltaKPIs(m.KPIs, p)
	}

	var inputs []analytics.ActivityInput
	for _, a := range in.Acts {
		if a.Status != jobs.StatusDone || a.Duration <= 0 {
			continue
		}
		m.Acts = append(m.Acts, a)
		h := in.HR[a.StravaID]
		inputs = append(inputs, analytics.ActivityInput{
			ID: a.StravaID, Sport: a.Sport, Start: a.Start, End: a.End(),
			AvgHR: h.Avg, MaxHR: h.Max, DistanceM: a.Distance, ElevationGain: a.ElevationGain,
		})
	}
	sort.SliceStable(m.Acts, func(i, j int) bool { return m.Acts[i].Start.After(m.Acts[j].Start) })
	m.Insights = analytics.InsightsFor(in.Samples, inputs, in.Thr, in.Loc)
	m.Sports = analytics.BySport(m.Insights)
	return m
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
