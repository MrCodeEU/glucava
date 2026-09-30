package analytics

import (
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// Windows used around an activity for the pre/post comparison and for the
// delayed-low check.
const (
	PreWindow            = 30 * time.Minute
	PostWindow           = 60 * time.Minute
	DefaultPostLowWithin = 3 * time.Hour
)

// ActivityInput is the part of an activity the insights need. The caller
// maps its own activity type onto it; zero HR, distance and elevation mean
// "not recorded".
type ActivityInput struct {
	ID            string
	Sport         string
	Start, End    time.Time
	AvgHR, MaxHR  float64 // bpm
	DistanceM     float64
	ElevationGain float64 // metres
}

// Duration is End - Start, never negative.
func (a ActivityInput) Duration() time.Duration {
	if a.End.Before(a.Start) {
		return 0
	}
	return a.End.Sub(a.Start)
}

// ActivityInsight is the glucose picture of one activity.
type ActivityInsight struct {
	ActivityInput
	// HasData is false when no reading falls inside the activity; every
	// glucose field below is then zero.
	HasData bool `json:"hasData"`

	StartGlucose float64 `json:"startGlucose"` // first reading during
	EndGlucose   float64 `json:"endGlucose"`   // last reading during
	Delta        float64 `json:"delta"`        // End - Start, mg/dL
	// DropRate is the fall from first to last reading in mg/dL per 10
	// minutes; positive means glucose fell, negative rose. Zero when the
	// first and last reading are the same instant.
	DropRate float64 `json:"dropRate"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Avg      float64 `json:"avg"`
	CV       float64 `json:"cv"`
	TIR      TIR5    `json:"tir"` // during the activity, sample-weighted

	// Mean glucose in the 30 minutes before and 60 minutes after; the Has
	// flags say whether any reading fell in the window. The mean during is
	// Avg.
	PreMean  float64 `json:"preMean"`
	HasPre   bool    `json:"hasPre"`
	PostMean float64 `json:"postMean"`
	HasPost  bool    `json:"hasPost"`

	// PostLows counts low episodes (below Low, at least 15 minutes) that
	// begin after the activity ended and within the delayed-low window.
	PostLows int `json:"postLows"`
	// PostLowNadir is the lowest reading of those episodes, 0 when none.
	PostLowNadir float64 `json:"postLowNadir"`

	// SpeedMS is distance over duration in m/s, 0 without distance.
	SpeedMS float64 `json:"speedMs"`
}

// PostLow reports whether a delayed low followed the activity.
func (i ActivityInsight) PostLow() bool { return i.PostLows > 0 }

// InsightOptions tunes InsightsWith; zero values mean the defaults.
type InsightOptions struct {
	PostLowWithin time.Duration // default DefaultPostLowWithin
}

// InsightsFor computes one insight per activity, in the order given, using
// the default delayed-low window.
func InsightsFor(samples []stats.Sample, acts []ActivityInput, thr Thresholds, loc *time.Location) []ActivityInsight {
	return InsightsWith(samples, acts, thr, loc, InsightOptions{})
}

// InsightsWith is InsightsFor with options. samples may be unsorted and may
// hold duplicates; an activity with no readings inside it still yields an
// insight (HasData false) so the caller can list it.
func InsightsWith(samples []stats.Sample, acts []ActivityInput, thr Thresholds, loc *time.Location, o InsightOptions) []ActivityInsight {
	if o.PostLowWithin <= 0 {
		o.PostLowWithin = DefaultPostLowWithin
	}
	if len(acts) == 0 {
		return nil
	}
	s := sortedCopy(samples)
	lows := OfKind(DetectEpisodes(s, thr, EpisodeOptions{Loc: loc}), KindLow)

	out := make([]ActivityInsight, len(acts))
	for i, a := range acts {
		out[i] = insightFor(s, lows, a, thr, o)
	}
	return out
}

// between returns the readings with from <= t <= to from sorted s.
func between(s []stats.Sample, from, to time.Time) []stats.Sample {
	lo := sort.Search(len(s), func(i int) bool { return !s[i].Time.Before(from) })
	hi := sort.Search(len(s), func(i int) bool { return s[i].Time.After(to) })
	if lo > hi {
		return nil
	}
	return s[lo:hi]
}

func meanOf(s []stats.Sample) (float64, bool) {
	if len(s) == 0 {
		return 0, false
	}
	var sum float64
	for _, x := range s {
		sum += x.Value
	}
	return sum / float64(len(s)), true
}

func insightFor(s []stats.Sample, lows []Episode, a ActivityInput, thr Thresholds, o InsightOptions) ActivityInsight {
	in := ActivityInsight{ActivityInput: a}
	if d := a.Duration(); d > 0 && a.DistanceM > 0 {
		in.SpeedMS = a.DistanceM / d.Seconds()
	}
	during := between(s, a.Start, a.End)
	// The pre window ends just before the start so a reading at the exact
	// start belongs to "during" only; likewise post begins after the end.
	pre := between(s, a.Start.Add(-PreWindow), a.Start.Add(-time.Nanosecond))
	post := between(s, a.End.Add(time.Nanosecond), a.End.Add(PostWindow))
	in.PreMean, in.HasPre = meanOf(pre)
	in.PostMean, in.HasPost = meanOf(post)

	if len(during) > 0 {
		b := Aggregate(during, thr)
		first, last := during[0], during[len(during)-1]
		in.HasData = true
		in.StartGlucose, in.EndGlucose = first.Value, last.Value
		in.Delta = last.Value - first.Value
		if span := last.Time.Sub(first.Time); span > 0 {
			in.DropRate = -in.Delta / span.Minutes() * 10
		}
		in.Min, in.Max, in.Avg, in.CV, in.TIR = b.Min, b.Max, b.Avg, b.CV, b.TIR
	}

	if eps := StartingAfter(lows, a.End, o.PostLowWithin); len(eps) > 0 {
		in.PostLows = len(eps)
		in.PostLowNadir = SummarizeEpisodes(eps).Extreme
	}
	return in
}

// SportStats aggregates the insights of one sport. Means are over the
// activities that have the data in question, so an activity without
// readings does not drag a mean to zero.
type SportStats struct {
	Sport    string  `json:"sport"`
	Count    int     `json:"count"`    // all activities
	WithData int     `json:"withData"` // activities with glucose readings
	TIR      float64 `json:"tir"`      // mean of per-activity in-range %
	Bands    TIR5    `json:"bands"`    // mean of each per-activity band, percent (sums to about 100)
	CV       float64 `json:"cv"`       // mean per-activity CV, percent
	Delta    float64 `json:"delta"`    // mean start-to-end change, mg/dL
	DropRate float64 `json:"dropRate"` // mean, mg/dL per 10 min, positive = falling
	// PostLowShare is the percentage of activities with data followed by a
	// delayed low.
	PostLowShare float64 `json:"postLowShare"`

	AvgHR          float64 `json:"avgHr"` // mean of the activities that recorded HR, 0 when none
	TotalDistance  float64 `json:"totalDistanceM"`
	AvgDistance    float64 `json:"avgDistanceM"` // over activities with a distance
	TotalElevation float64 `json:"totalElevationM"`
	// AvgSpeedMS is total distance over total duration of the activities
	// with a distance, in m/s. Callers format it as pace or speed by sport
	// (render.FormatPace does that from distance and duration).
	AvgSpeedMS float64 `json:"avgSpeedMs"`
	// Duration is the summed duration of all activities.
	Duration time.Duration `json:"duration"`
}

// BySport groups insights by sport, most activities first (ties by name).
func BySport(insights []ActivityInsight) []SportStats {
	type acc struct {
		st                   SportStats
		tir, cv, delta, drop float64
		bands                TIR5
		postLow              int
		hr                   float64
		hrN, distN           int
		distDur              time.Duration
		distTotal            float64
	}
	m := map[string]*acc{}
	var order []string
	for _, in := range insights {
		a := m[in.Sport]
		if a == nil {
			a = &acc{st: SportStats{Sport: in.Sport}}
			m[in.Sport] = a
			order = append(order, in.Sport)
		}
		a.st.Count++
		a.st.Duration += in.Duration()
		if in.HasData {
			a.st.WithData++
			a.tir += in.TIR.InRange
			a.bands.VeryLow += in.TIR.VeryLow
			a.bands.Low += in.TIR.Low
			a.bands.InRange += in.TIR.InRange
			a.bands.High += in.TIR.High
			a.bands.VeryHigh += in.TIR.VeryHigh
			a.cv += in.CV
			a.delta += in.Delta
			a.drop += in.DropRate
			if in.PostLow() {
				a.postLow++
			}
		}
		if in.AvgHR > 0 {
			a.hr += in.AvgHR
			a.hrN++
		}
		if in.DistanceM > 0 {
			a.distN++
			a.distTotal += in.DistanceM
			a.distDur += in.Duration()
		}
		a.st.TotalElevation += in.ElevationGain
	}
	out := make([]SportStats, 0, len(order))
	for _, k := range order {
		a := m[k]
		st := a.st
		if n := float64(st.WithData); n > 0 {
			st.TIR, st.CV, st.Delta, st.DropRate = a.tir/n, a.cv/n, a.delta/n, a.drop/n
			st.PostLowShare = float64(a.postLow) / n * 100
			st.Bands = TIR5{VeryLow: a.bands.VeryLow / n, Low: a.bands.Low / n, InRange: a.bands.InRange / n,
				High: a.bands.High / n, VeryHigh: a.bands.VeryHigh / n, Count: st.WithData}
		}
		if a.hrN > 0 {
			st.AvgHR = a.hr / float64(a.hrN)
		}
		st.TotalDistance = a.distTotal
		if a.distN > 0 {
			st.AvgDistance = a.distTotal / float64(a.distN)
		}
		if a.distDur > 0 {
			st.AvgSpeedMS = a.distTotal / a.distDur.Seconds()
		}
		out = append(out, st)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Sport < out[j].Sport
	})
	return out
}

// ScatterPoint is one activity in the start-glucose versus change plot.
type ScatterPoint struct {
	ID    string
	Sport string
	Start time.Time
	X     float64 // start glucose, mg/dL
	Y     float64 // change during the activity, mg/dL
	TIR   float64 // in-range %, for colouring
}

// Scatter returns one point per activity that has readings, in input order.
func Scatter(insights []ActivityInsight) []ScatterPoint {
	var out []ScatterPoint
	for _, in := range insights {
		if !in.HasData {
			continue
		}
		out = append(out, ScatterPoint{ID: in.ID, Sport: in.Sport, Start: in.Start,
			X: in.StartGlucose, Y: in.Delta, TIR: in.TIR.InRange})
	}
	return out
}

// BestWorst returns the n activities with the highest and the n with the
// lowest in-range share among those with readings, best first and worst
// first. The two lists never share an activity: with fewer than 2n
// candidates the worst list shrinks.
func BestWorst(insights []ActivityInsight, n int) (best, worst []ActivityInsight) {
	var c []ActivityInsight
	for _, in := range insights {
		if in.HasData {
			c = append(c, in)
		}
	}
	if n <= 0 || len(c) == 0 {
		return nil, nil
	}
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].TIR.InRange != c[j].TIR.InRange {
			return c[i].TIR.InRange > c[j].TIR.InRange
		}
		return c[i].Start.Before(c[j].Start)
	})
	nb := min(n, len(c))
	nw := min(n, len(c)-nb)
	best = append(best, c[:nb]...)
	for i := 0; i < nw; i++ {
		worst = append(worst, c[len(c)-1-i])
	}
	return best, worst
}

// KPIs are the headline numbers of a period.
type KPIs struct {
	Count    int     `json:"count"` // readings
	TIR      float64 `json:"tir"`   // in-range %
	GMI      float64 `json:"gmi"`
	Avg      float64 `json:"avg"`
	CV       float64 `json:"cv"`
	GRI      float64 `json:"gri"`
	GRIZone  string  `json:"griZone"`
	Coverage float64 `json:"coverage"` // percent
	Days     int     `json:"days"`     // local days with at least one reading
}

// ComputeKPIs summarizes the readings in [from, to]. Zero KPIs (Count 0) for
// none.
func ComputeKPIs(samples []stats.Sample, thr Thresholds, from, to time.Time, loc *time.Location) KPIs {
	s := sortedWithin(samples, from, to)
	if len(s) == 0 {
		return KPIs{}
	}
	b := Aggregate(s, thr)
	gri := ComputeGRI(b.TIR)
	return KPIs{
		Count: b.Count, TIR: b.TIR.InRange, GMI: 3.31 + 0.02392*b.Avg, Avg: b.Avg, CV: b.CV,
		GRI: gri.Score, GRIZone: gri.Zone,
		Coverage: ComputeCoverage(s, from, to, 0, 0).Pct,
		Days:     len(ComputeDaily(s, thr, loc)),
	}
}

// KPIDelta is current minus previous for each KPI. Valid is false when
// either period has no readings, and the caller should not show a delta.
type KPIDelta struct {
	Valid    bool
	TIR      float64
	GMI      float64
	Avg      float64
	CV       float64
	GRI      float64
	Coverage float64
	Days     int
}

// DeltaKPIs is cur - prev.
func DeltaKPIs(cur, prev KPIs) KPIDelta {
	if cur.Count == 0 || prev.Count == 0 {
		return KPIDelta{}
	}
	return KPIDelta{Valid: true, TIR: cur.TIR - prev.TIR, GMI: cur.GMI - prev.GMI, Avg: cur.Avg - prev.Avg,
		CV: cur.CV - prev.CV, GRI: cur.GRI - prev.GRI, Coverage: cur.Coverage - prev.Coverage, Days: cur.Days - prev.Days}
}

// PreviousPeriod returns the range of the same length that ends right
// before from: [from-(to-from), from). The end is one nanosecond short of
// from so a reading exactly at from is counted in the current period only,
// since ComputeKPIs treats both ends as inclusive.
func PreviousPeriod(from, to time.Time) (prevFrom, prevTo time.Time) {
	d := to.Sub(from)
	if d < 0 {
		d = 0
	}
	return from.Add(-d), from.Add(-time.Nanosecond)
}
