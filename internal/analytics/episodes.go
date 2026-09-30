package analytics

import (
	"math"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// EpisodeKind names a threshold crossing. The kinds are detected
// independently, so a very-low episode is nested inside the wider low
// episode that contains it; filter to one kind for a non-overlapping list.
type EpisodeKind string

// The kinds.
const (
	KindVeryLow  EpisodeKind = "veryLow"  // below VeryLow
	KindLow      EpisodeKind = "low"      // below Low
	KindHigh     EpisodeKind = "high"     // above High
	KindVeryHigh EpisodeKind = "veryHigh" // above VeryHigh
)

// Episode defaults, from the consensus definition (>= 15 minutes) and a
// tolerance of about four missed 5-minute readings.
const (
	DefaultEpisodeMinDuration = 15 * time.Minute
	DefaultEpisodeMaxGap      = 20 * time.Minute
)

// NocturnalEndHour is the local hour at which the night (00:00 to here) ends.
const NocturnalEndHour = 6

// EpisodeOptions tunes DetectEpisodes; zero values mean the defaults.
type EpisodeOptions struct {
	MinDuration time.Duration
	MaxGap      time.Duration
	Loc         *time.Location // for the Nocturnal flag; nil means UTC
}

// Episode is one run of readings beyond a threshold.
type Episode struct {
	Kind  EpisodeKind `json:"kind"`
	Start time.Time   `json:"start"` // first reading in the run
	End   time.Time   `json:"end"`   // last reading in the run
	// Duration is End - Start: the time between the first and last reading
	// beyond the threshold. Three 5-minute readings span 10 minutes; four
	// span 15 and are the shortest run that qualifies by default.
	Duration time.Duration `json:"duration"`
	Count    int           `json:"count"`
	// Extreme is the lowest reading for low kinds, the highest for high kinds.
	Extreme float64 `json:"extreme"`
	// Nocturnal is true when Start falls between local 00:00 and 06:00.
	Nocturnal bool `json:"nocturnal"`
}

// DetectEpisodes finds the episodes of every kind, ordered by Start (ties:
// veryLow, low, high, veryHigh). A run ends at the first reading back inside
// the threshold or after a silence longer than MaxGap; a run whose
// Duration is under MinDuration is dropped, so one lone reading never
// qualifies.
func DetectEpisodes(samples []stats.Sample, th Thresholds, o EpisodeOptions) []Episode {
	if o.MinDuration <= 0 {
		o.MinDuration = DefaultEpisodeMinDuration
	}
	if o.MaxGap <= 0 {
		o.MaxGap = DefaultEpisodeMaxGap
	}
	loc := o.Loc
	if loc == nil {
		loc = time.UTC
	}
	s := sortedCopy(samples)

	type detector struct {
		kind   EpisodeKind
		beyond func(float64) bool
		better func(a, b float64) bool // a more extreme than b
	}
	dets := []detector{
		{KindVeryLow, func(v float64) bool { return v < th.VeryLow }, func(a, b float64) bool { return a < b }},
		{KindLow, func(v float64) bool { return v < th.Low }, func(a, b float64) bool { return a < b }},
		{KindHigh, func(v float64) bool { return v > th.High }, func(a, b float64) bool { return a > b }},
		{KindVeryHigh, func(v float64) bool { return v > th.VeryHigh }, func(a, b float64) bool { return a > b }},
	}
	var out []Episode
	for _, d := range dets {
		var cur *Episode
		flush := func() {
			if cur != nil && cur.Duration >= o.MinDuration {
				h := cur.Start.In(loc).Hour()
				cur.Nocturnal = h < NocturnalEndHour
				out = append(out, *cur)
			}
			cur = nil
		}
		for _, p := range s {
			if !d.beyond(p.Value) {
				flush()
				continue
			}
			if cur != nil && p.Time.Sub(cur.End) > o.MaxGap {
				flush()
			}
			if cur == nil {
				cur = &Episode{Kind: d.kind, Start: p.Time, End: p.Time, Extreme: p.Value}
			}
			cur.End = p.Time
			cur.Duration = cur.End.Sub(cur.Start)
			cur.Count++
			if d.better(p.Value, cur.Extreme) {
				cur.Extreme = p.Value
			}
		}
		flush()
	}
	rank := map[EpisodeKind]int{KindVeryLow: 0, KindLow: 1, KindHigh: 2, KindVeryHigh: 3}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return rank[out[i].Kind] < rank[out[j].Kind]
	})
	return out
}

// OfKind returns the episodes of one kind, order preserved.
func OfKind(eps []Episode, k EpisodeKind) []Episode {
	var out []Episode
	for _, e := range eps {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

// StartingAfter returns the episodes that begin after end and no later than
// end+within, for delayed post-activity lows. Episodes that were already
// running when the activity ended are not included.
func StartingAfter(eps []Episode, end time.Time, within time.Duration) []Episode {
	var out []Episode
	for _, e := range eps {
		if e.Start.After(end) && !e.Start.After(end.Add(within)) {
			out = append(out, e)
		}
	}
	return out
}

// EpisodeStats summarizes a list of episodes of one kind over a period.
type EpisodeStats struct {
	Count     int           `json:"count"`
	Nocturnal int           `json:"nocturnal"`
	Total     time.Duration `json:"total"`
	Longest   time.Duration `json:"longest"`
	Extreme   float64       `json:"extreme"` // 0 when Count == 0
}

// SummarizeEpisodes aggregates eps (which should all be one kind).
func SummarizeEpisodes(eps []Episode) EpisodeStats {
	var st EpisodeStats
	for i, e := range eps {
		st.Count++
		if e.Nocturnal {
			st.Nocturnal++
		}
		st.Total += e.Duration
		st.Longest = max(st.Longest, e.Duration)
		if i == 0 {
			st.Extreme = e.Extreme
		} else if e.Kind == KindLow || e.Kind == KindVeryLow {
			st.Extreme = math.Min(st.Extreme, e.Extreme)
		} else {
			st.Extreme = math.Max(st.Extreme, e.Extreme)
		}
	}
	return st
}
