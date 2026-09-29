// Package overview aggregates already-processed activities into the trends
// the stats overview page shows: day-by-day TIR/average/CV, and a per-sport
// breakdown. It works entirely from each activity's own stats.Summary,
// computed once by the normal pipeline, rather than re-scanning raw glucose
// samples for the whole range.
package overview

import (
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// TrendPoint is one calendar day's aggregate across the activities that
// finished that day. Day is local midnight in the location Build was given.
type TrendPoint struct {
	Day   time.Time
	TIR   float64 // mean of that day's activities' TIR
	Avg   float64 // mean of that day's activities' average glucose
	CV    float64 // mean of that day's activities' coefficient of variation
	Count int
}

// SportStats is one activity type's aggregate over the whole range.
type SportStats struct {
	Sport      string
	Count      int
	AvgTIR     float64
	AvgGlucose float64
}

// Data is everything the stats overview page needs, already computed.
type Data struct {
	Trend      []TrendPoint    // chronological, oldest first
	BySport    []SportStats    // sorted by Count descending, then Sport
	Activities []jobs.Activity // as given, for the raw table
}

// Build aggregates acts (any status) into Data. Only activities with
// Status == jobs.StatusDone and a non-nil Summary contribute to Trend and
// BySport; Activities keeps the full list as given, so the raw table can
// still show pending/failed rows. loc buckets each activity into a calendar
// day in that location, matching how the rest of the UI shows times.
func Build(acts []jobs.Activity, loc *time.Location) Data {
	type acc struct {
		tirSum, avgSum, cvSum float64
		n                     int
	}
	byDay := map[time.Time]*acc{}
	bySport := map[string]*acc{}

	for _, a := range acts {
		if a.Status != jobs.StatusDone || a.Summary == nil {
			continue
		}
		sum := a.Summary

		day := dayOf(a.Start, loc)
		d := byDay[day]
		if d == nil {
			d = &acc{}
			byDay[day] = d
		}
		d.tirSum += sum.TIR
		d.avgSum += sum.Avg
		d.cvSum += sum.CV
		d.n++

		sport := a.Sport
		if sport == "" {
			sport = "Other"
		}
		sp := bySport[sport]
		if sp == nil {
			sp = &acc{}
			bySport[sport] = sp
		}
		sp.tirSum += sum.TIR
		sp.avgSum += sum.Avg
		sp.n++
	}

	days := make([]time.Time, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	trend := make([]TrendPoint, len(days))
	for i, day := range days {
		a := byDay[day]
		trend[i] = TrendPoint{
			Day: day, Count: a.n,
			TIR: a.tirSum / float64(a.n), Avg: a.avgSum / float64(a.n), CV: a.cvSum / float64(a.n),
		}
	}

	sports := make([]SportStats, 0, len(bySport))
	for name, a := range bySport {
		sports = append(sports, SportStats{
			Sport: name, Count: a.n,
			AvgTIR: a.tirSum / float64(a.n), AvgGlucose: a.avgSum / float64(a.n),
		})
	}
	sort.Slice(sports, func(i, j int) bool {
		if sports[i].Count != sports[j].Count {
			return sports[i].Count > sports[j].Count
		}
		return sports[i].Sport < sports[j].Sport
	})

	return Data{Trend: trend, BySport: sports, Activities: acts}
}

// GeneralData is the whole-range glucose picture, independent of any
// activity: every stored reading in the range, not just the ones around a
// workout.
type GeneralData struct {
	Overall stats.Summary // over every sample in the range
	HasData bool          // false when there were no samples at all
	Trend   []TrendPoint  // one point per calendar day that has at least one sample
}

// BuildGeneral aggregates every glucose sample in the range (regardless of
// whether it falls near an activity) into a whole-range summary and a
// day-by-day trend, bucketed the same way Build buckets activities.
func BuildGeneral(samples []stats.Sample, rng stats.Range, loc *time.Location) GeneralData {
	overall, ok := stats.Summarize(samples, rng)
	if !ok {
		return GeneralData{}
	}

	byDay := map[time.Time][]stats.Sample{}
	for _, s := range samples {
		day := dayOf(s.Time, loc)
		byDay[day] = append(byDay[day], s)
	}
	days := make([]time.Time, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })

	trend := make([]TrendPoint, len(days))
	for i, day := range days {
		sum, _ := stats.Summarize(byDay[day], rng)
		trend[i] = TrendPoint{Day: day, Count: len(byDay[day]), TIR: sum.TIR, Avg: sum.Avg, CV: sum.CV}
	}

	return GeneralData{Overall: overall, HasData: true, Trend: trend}
}

// dayOf returns local midnight for t in loc. Not t.Truncate(24*time.Hour):
// Truncate rounds an absolute duration since the Unix epoch, which does not
// line up with local calendar days wherever loc's offset isn't a multiple
// of 24h (most of the world), and would misbucket around a DST transition.
func dayOf(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
