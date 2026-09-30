package analytics

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// This file flags suspected CGM artifacts: readings that look like a sensor
// problem rather than real glucose. It only flags. Callers decide whether to
// show a span as suspected (the default) or to leave it out of statistics
// with ExcludeArtifacts, and the person can override any call with marks
// (ApplyMarks). The rules are heuristics over the shape of the trace, so they
// have honest limits: a real fast low that happens to recover quickly at night
// can look like a compression low.

// ArtifactKind says which pattern a span matched.
type ArtifactKind string

// The kinds.
const (
	// ArtifactCompression is a low that appears and disappears fast, at
	// night: the sensor was pressed while lying on it.
	ArtifactCompression ArtifactKind = "compression"
	// ArtifactDip is an abrupt fall of a few tens of mg/dL for a short
	// while, often with a signal gap, then a return to the previous level.
	ArtifactDip ArtifactKind = "dip"
	// ArtifactMarked is a span the person marked as not real by hand where
	// the detector saw nothing.
	ArtifactMarked ArtifactKind = "marked"
)

// Tuning constants. They are deliberately conservative: a real low should
// not be flagged, and a missed artifact costs only a slightly high count.
const (
	// DipMinDrop is the smallest fall that can be a dip, in mg/dL, within
	// DipDropWindow.
	DipMinDrop    = 25.0
	DipDropWindow = 10 * time.Minute
	// DipMaxStay is the longest a dip may last, from the first low reading
	// to the first recovered one, gap included.
	DipMaxStay = 35 * time.Minute
	// DipRecoveryTolerance is how close to the pre-fall level a reading
	// must come back, in mg/dL, for the dip to count as recovered.
	DipRecoveryTolerance = 15.0
	// DipMinRecoveryRate is the slowest average rise, mg/dL per minute from
	// the lowest reading to the recovered one, that still counts as a
	// sensor recovery. A slow climb back is treated like a real low.
	DipMinRecoveryRate = 1.5

	// CompressionMinRate is the smallest onset fall and recovery rise, in
	// mg/dL per minute.
	CompressionMinRate = 2.0
	// CompressionMinDrop is the smallest total fall from the last normal
	// reading to the nadir, in mg/dL.
	CompressionMinDrop = 20.0
	// CompressionMaxDuration is the longest low that can be a compression
	// low. Longer lows are treated as real.
	CompressionMaxDuration = 60 * time.Minute

	// DefaultNightStartHour and DefaultNightEndHour bound the local night in
	// which compression lows are looked for (22:00 to 08:00).
	DefaultNightStartHour = 22
	DefaultNightEndHour   = 8

	// DefaultMinConfidence keeps every span that matched a rule.
	DefaultMinConfidence = 0.5

	// artifactStepGap is the longest time between two readings that still
	// count as neighbours when a rate of change is computed.
	artifactStepGap = 15 * time.Minute
	// artifactGapNote is the silence that counts as a signal gap for the
	// confidence bonus.
	artifactGapNote = 8 * time.Minute
)

// ArtifactOptions tunes DetectArtifacts; zero values mean the defaults.
type ArtifactOptions struct {
	// Loc decides which hours are night; nil means UTC.
	Loc *time.Location
	// NightStartHour and NightEndHour are the local night, start inclusive,
	// end exclusive, and may wrap midnight. Both zero means 22 and 8.
	NightStartHour, NightEndHour int
	// MinConfidence drops spans below it; zero means DefaultMinConfidence.
	MinConfidence float64
}

// Artifact is one suspected sensor artifact.
type Artifact struct {
	Kind ArtifactKind `json:"kind"`
	// Start and End are the first and last affected reading; ExcludeArtifacts
	// removes readings inside [Start, End].
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// Reason is a short plain-language explanation.
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"` // 0.5 to 1
	Nadir      float64 `json:"nadir"`      // lowest reading in the span, mg/dL
	PreLevel   float64 `json:"preLevel"`   // the reading before it, mg/dL
	PostLevel  float64 `json:"postLevel"`  // the first reading after it, mg/dL
	// Marked is true when a person confirmed the span by hand.
	Marked bool `json:"marked,omitempty"`
}

// Overlaps reports whether the span shares any time with [from, to].
func (a Artifact) Overlaps(from, to time.Time) bool {
	return !a.End.Before(from) && !a.Start.After(to)
}

// OverlappingArtifact returns the first span overlapping [from, to].
func OverlappingArtifact(spans []Artifact, from, to time.Time) (Artifact, bool) {
	for _, a := range spans {
		if a.Overlaps(from, to) {
			return a, true
		}
	}
	return Artifact{}, false
}

// inNight reports whether t falls in the local night window.
func inNight(t time.Time, loc *time.Location, from, to int) bool {
	h := t.In(loc).Hour()
	if from == to {
		return false
	}
	if from < to {
		return h >= from && h < to
	}
	return h >= from || h < to // wraps midnight
}

func minutes(d time.Duration) float64 { return d.Minutes() }

// DetectArtifacts finds suspected sensor artifacts, ordered by Start.
//
// A compression low is a run of readings below thr.Low that starts in the
// local night, lasts at most CompressionMaxDuration, falls in at
// CompressionMinRate or faster from a reading no more than 15 minutes
// before, and recovers at that rate or faster (or returns to the prior level
// across a signal gap).
//
// A dip is a fall of DipMinDrop within DipDropWindow that comes back to
// within DipRecoveryTolerance of the earlier level within DipMaxStay, at
// least DipMinRecoveryRate on average. It is looked for at any time of day
// and at any level, and only where no compression low was found.
//
// A real low falls more slowly, stays longer, or climbs back slowly, and is
// left alone.
func DetectArtifacts(samples []stats.Sample, thr Thresholds, o ArtifactOptions) []Artifact {
	loc := o.Loc
	if loc == nil {
		loc = time.UTC
	}
	from, to := o.NightStartHour, o.NightEndHour
	if from == 0 && to == 0 {
		from, to = DefaultNightStartHour, DefaultNightEndHour
	}
	minConf := o.MinConfidence
	if minConf <= 0 {
		minConf = DefaultMinConfidence
	}
	s := sortedCopy(samples)

	out := detectCompression(s, thr, loc, from, to)
	for _, d := range detectDips(s) {
		if _, clash := OverlappingArtifact(out, d.Start, d.End); !clash {
			out = append(out, d)
		}
	}
	kept := out[:0]
	for _, a := range out {
		if a.Confidence >= minConf {
			kept = append(kept, a)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Start.Before(kept[j].Start) })
	return kept
}

func detectCompression(s []stats.Sample, thr Thresholds, loc *time.Location, nightFrom, nightTo int) []Artifact {
	var out []Artifact
	n := len(s)
	for a := 0; a < n; a++ {
		if s[a].Value >= thr.Low {
			continue
		}
		b := a
		for b+1 < n && s[b+1].Value < thr.Low && s[b+1].Time.Sub(s[b].Time) <= DefaultEpisodeMaxGap {
			b++
		}
		start, end := a, b
		a = b // continue after the run whatever the verdict
		if s[end].Time.Sub(s[start].Time) > CompressionMaxDuration || !inNight(s[start].Time, loc, nightFrom, nightTo) {
			continue
		}
		if start == 0 || s[start].Time.Sub(s[start-1].Time) > artifactStepGap || end+1 >= n {
			continue // no known level before, or no recovery to judge
		}
		pre := s[start-1].Value
		m := start
		for i := start; i <= end; i++ {
			if s[i].Value <= s[m].Value { // the last lowest reading: recovery starts from there
				m = i
			}
		}
		nadir := s[m].Value
		if pre-nadir < CompressionMinDrop {
			continue
		}
		onset := (pre - s[start].Value) / minutes(s[start].Time.Sub(s[start-1].Time))
		if start >= 2 && s[start].Time.Sub(s[start-2].Time) <= artifactStepGap {
			if two := (s[start-2].Value - s[start].Value) / minutes(s[start].Time.Sub(s[start-2].Time)); two > onset {
				onset = two
			}
		}
		if onset < CompressionMinRate {
			continue
		}
		r := end + 1
		post := s[r].Value
		gapBefore := s[start].Time.Sub(s[start-1].Time) >= artifactGapNote
		gapAfter := s[r].Time.Sub(s[end].Time) >= artifactGapNote
		var rec float64
		switch {
		case s[r].Time.Sub(s[end].Time) <= DefaultEpisodeMaxGap && post >= thr.Low:
			rec = (post - nadir) / minutes(s[r].Time.Sub(s[m].Time))
			if rec < CompressionMinRate {
				continue
			}
		case gapAfter && post >= thr.Low && post >= pre-DipRecoveryTolerance:
			rec = math.Inf(1) // came back across a signal gap
		default:
			continue
		}
		conf := 0.5
		if onset >= 3 {
			conf += 0.15
		}
		if rec >= 3 {
			conf += 0.15
		}
		if s[end].Time.Sub(s[start].Time) <= 45*time.Minute {
			conf += 0.1
		}
		if gapBefore || gapAfter {
			conf += 0.1
		}
		out = append(out, Artifact{
			Kind: ArtifactCompression, Start: s[start].Time, End: s[end].Time,
			Reason: fmt.Sprintf("fast fall (%.1f mg/dL per min) and fast recovery, %d min, overnight",
				onset, int(s[end].Time.Sub(s[start].Time).Minutes())),
			Confidence: math.Min(conf, 1), Nadir: nadir, PreLevel: pre, PostLevel: post,
		})
	}
	return out
}

func detectDips(s []stats.Sample) []Artifact {
	var out []Artifact
	n := len(s)
	for i := 0; i+1 < n; i++ {
		pre := s[i].Value
		j := -1
		for x := i + 1; x < n && s[x].Time.Sub(s[i].Time) <= DipDropWindow; x++ {
			if pre-s[x].Value >= DipMinDrop {
				j = x
				break
			}
		}
		if j < 0 {
			continue
		}
		for x := i + 1; x < j; x++ { // the level the fall started from
			pre = math.Max(pre, s[x].Value)
		}
		// Find the first reading back near the earlier level, within the stay limit.
		k := -1
		for x := j + 1; x < n && s[x].Time.Sub(s[j].Time) <= DipMaxStay; x++ {
			if s[x].Value >= pre-DipRecoveryTolerance {
				k = x
				break
			}
		}
		if k < 0 {
			continue
		}
		m := j
		for x := j; x < k; x++ {
			if s[x].Value <= s[m].Value {
				m = x
			}
		}
		nadir := s[m].Value
		if (s[k].Value-nadir)/minutes(s[k].Time.Sub(s[m].Time)) < DipMinRecoveryRate {
			continue
		}
		conf := 0.5
		if pre-nadir >= 30 {
			conf += 0.15
		}
		if math.Abs(s[k].Value-pre) <= 8 {
			conf += 0.15
		}
		if s[k].Time.Sub(s[k-1].Time) >= artifactGapNote {
			conf += 0.15
		}
		out = append(out, Artifact{
			Kind: ArtifactDip, Start: s[j].Time, End: s[k-1].Time,
			Reason: fmt.Sprintf("sudden drop of %.0f mg/dL for %d min, then back to the earlier level",
				pre-nadir, int(s[k].Time.Sub(s[j].Time).Minutes())),
			Confidence: math.Min(conf, 1), Nadir: nadir, PreLevel: pre, PostLevel: s[k].Value,
		})
		i = k - 1
	}
	return out
}

// ExcludeArtifacts returns samples without the readings inside any span,
// so they act like a signal gap. Order is kept; samples is not modified.
func ExcludeArtifacts(samples []stats.Sample, spans []Artifact) []stats.Sample {
	if len(spans) == 0 {
		return samples
	}
	out := make([]stats.Sample, 0, len(samples))
	for _, x := range samples {
		drop := false
		for _, a := range spans {
			if !x.Time.Before(a.Start) && !x.Time.After(a.End) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, x)
		}
	}
	return out
}

// MarkKind is a person's verdict on a span.
type MarkKind string

// The verdicts.
const (
	MarkArtifact MarkKind = "artifact" // "not real"
	MarkReal     MarkKind = "real"     // "this was real"
)

// Mark is a manual verdict over [Start, End].
type Mark struct {
	Start time.Time
	End   time.Time
	Kind  MarkKind
}

// ApplyMarks merges manual verdicts into detected spans. Marks are applied in
// the order given, so a later one wins. A "real" mark removes every span it
// overlaps. An "artifact" mark confirms the spans it overlaps (confidence 1,
// Marked) or, when there are none, adds a new ArtifactMarked span with its
// levels read from samples.
func ApplyMarks(spans []Artifact, marks []Mark, samples []stats.Sample) []Artifact {
	out := append([]Artifact(nil), spans...)
	s := sortedCopy(samples)
	for _, m := range marks {
		switch m.Kind {
		case MarkReal:
			kept := out[:0]
			for _, a := range out {
				if !a.Overlaps(m.Start, m.End) {
					kept = append(kept, a)
				}
			}
			out = kept
		case MarkArtifact:
			hit := false
			for i := range out {
				if out[i].Overlaps(m.Start, m.End) {
					out[i].Confidence, out[i].Marked = 1, true
					hit = true
				}
			}
			if !hit {
				out = append(out, markedSpan(s, m))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

func markedSpan(s []stats.Sample, m Mark) Artifact {
	a := Artifact{Kind: ArtifactMarked, Start: m.Start, End: m.End, Reason: "marked by you", Confidence: 1, Marked: true}
	lo := sort.Search(len(s), func(i int) bool { return !s[i].Time.Before(m.Start) })
	hi := sort.Search(len(s), func(i int) bool { return s[i].Time.After(m.End) })
	if lo < hi {
		a.Nadir = s[lo].Value
		for _, x := range s[lo:hi] {
			a.Nadir = math.Min(a.Nadir, x.Value)
		}
	}
	if lo > 0 {
		a.PreLevel = s[lo-1].Value
	}
	if hi < len(s) {
		a.PostLevel = s[hi].Value
	}
	return a
}
