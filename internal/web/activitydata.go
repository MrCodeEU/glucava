package web

import (
	"context"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// ActivityRef points at a neighbouring activity for the prev/next buttons.
type ActivityRef struct {
	ID, Name string
	Start    time.Time
}

// SportRank says how an activity's time in range compares with the user's
// other activities of the same sport.
type SportRank struct {
	Sport   string
	Percent float64 // share of the others it beats, 0-100
	Others  int
}

// minRankOthers is how many other activities of a sport make a percentile
// meaningful; below it the tile stays away rather than say "better than 100%
// of 1 run".
const minRankOthers = 4

// activityNeighbours picks the activity before and after act by start time
// from every stored activity, and ranks act's time in range among the others
// of its sport. all may be in any order and may contain act itself.
func activityNeighbours(all []jobs.Activity, act jobs.Activity) (prev, next *ActivityRef, rank *SportRank) {
	sorted := append([]jobs.Activity(nil), all...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	var others []float64
	for i := range sorted {
		a := sorted[i]
		if a.StravaID == act.StravaID {
			continue
		}
		ref := &ActivityRef{ID: a.StravaID, Name: a.Name, Start: a.Start}
		switch {
		case a.Start.Before(act.Start):
			prev = ref // ascending order: the last one wins, the closest before
		case next == nil:
			next = ref
		}
		if a.Sport == act.Sport && a.Status == jobs.StatusDone && a.Summary != nil && a.Summary.Count > 0 {
			others = append(others, a.Summary.TIR)
		}
	}
	if act.Summary != nil && len(others) >= minRankOthers {
		if pct, ok := analytics.PercentRank(others, act.Summary.TIR); ok {
			rank = &SportRank{Sport: act.Sport, Percent: pct, Others: len(others)}
		}
	}
	return prev, next, rank
}

// activityInsight computes the before/during/after numbers of one activity
// from readings that reach far enough past its end to spot a delayed low.
func activityInsight(samples []stats.Sample, act jobs.Activity, hr stats.HRSummary, hasHR bool, thr analytics.Thresholds, loc *time.Location) *analytics.ActivityInsight {
	in := analytics.ActivityInput{
		ID: act.StravaID, Sport: act.Sport, Start: act.Start, End: act.End(),
		DistanceM: act.Distance, ElevationGain: act.ElevationGain,
	}
	if hasHR {
		in.AvgHR, in.MaxHR = hr.Avg, hr.Max
	}
	res := analytics.InsightsFor(samples, []analytics.ActivityInput{in}, thr, loc)
	if len(res) == 0 || !res[0].HasData {
		return nil
	}
	return &res[0]
}

// loadActivityContext fetches what the activity page needs beyond the
// activity itself: all activities (light, no streams) for navigation and
// ranking, and readings out to the delayed-low window.
func (s *Server) loadActivityContext(ctx context.Context, act jobs.Activity) (all []jobs.Activity, insightSamples []stats.Sample, err error) {
	all, err = s.Store.ActivitiesInRangeLight(ctx, time.Unix(0, 0), s.now().Add(48*time.Hour))
	if err != nil {
		return nil, nil, err
	}
	insightSamples, err = s.Store.LoadSamplesFast(ctx,
		act.Start.Add(-analytics.PreWindow), act.End().Add(analytics.DefaultPostLowWithin))
	return all, insightSamples, err
}
