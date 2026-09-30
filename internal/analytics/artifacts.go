package analytics

import (
	"fmt"
	"math"
	"sort"
	"strings"
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
//
// Direction matters: only downward steps are ever flagged. A sudden rise is
// easy to explain (food, adrenaline) and is never treated as an artifact.

// ArtifactKind says which pattern a span matched.
type ArtifactKind string

// The kinds.
const (
	// ArtifactCompression is a low that appears and disappears fast, at
	// night: the sensor was pressed while lying on it.
	ArtifactCompression ArtifactKind = "compression"
	// ArtifactDip is an abrupt fall of a few tens of mg/dL that stays
	// depressed for a few readings, often with a signal gap, then returns to
	// the earlier level.
	ArtifactDip ArtifactKind = "dip"
	// ArtifactMarked is a span the person marked as not real by hand where
	// the detector saw nothing.
	ArtifactMarked ArtifactKind = "marked"
)

// Tuning constants. They are deliberately conservative: a real low should
// not be flagged, and a missed artifact costs only a slightly high count.
const (
	// DipMinDrop is the smallest fall below the baseline that can be a dip,
	// in mg/dL.
	DipMinDrop = 25.0
	// DipDropWindow is how long the fall may take: at most two 5-minute
	// readings, with a minute of slack for timestamp jitter.
	DipDropWindow = 11 * time.Minute
	// DipBaselineSpan is how much history before the fall the baseline (the
	// median of those readings) is taken from.
	DipBaselineSpan = 15 * time.Minute
	// DipStayFraction is how far below the baseline, as a share of the fall,
	// a reading must stay to count as still depressed.
	DipStayFraction = 0.6
	// DipPlateauBand is how far a depressed reading may stray from the first
	// one, in mg/dL: a sensor dip is flat, a real low drifts.
	DipPlateauBand = 12.0
	// DipMaxStayReadings and DipMaxStay bound how long the depressed stretch
	// may last (about 3 readings is typical, 7 the most).
	DipMaxStayReadings = 7
	DipMaxStay         = 35 * time.Minute
	// DipMaxRamp is the most readings the climb back may take before a
	// reading is again within DipRecoveryTolerance of the baseline.
	DipMaxRamp = 2
	// DipRecoveryTolerance is how close to the baseline a reading must come
	// back, in mg/dL, for the dip to count as recovered.
	DipRecoveryTolerance = 15.0
	// DipMinRecoveryRate is the slowest average rise, mg/dL per minute from
	// the last depressed reading to the recovered one, that still counts as
	// a sensor recovery. A slow climb back is treated like a real low.
	DipMinRecoveryRate = 1.5
	// dipMaxRecoveryGap is the longest silence between the last depressed
	// reading and the recovered one.
	dipMaxRecoveryGap = 25 * time.Minute
	// dipBaselineNoise is how far a reading may sit from the baseline and
	// still count as "at the baseline" just before the fall.
	dipBaselineNoise = 6.0

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

	// DefaultMinConfidence keeps every span that matched a rule and was not
	// pushed below this by the doubts (activity, slow decline).
	DefaultMinConfidence = 0.5

	// A fall faster than this is more likely a sensor than the body:
	// interstitial glucose rarely drops that quickly.
	artifactFastFall = 3.0 // mg/dL per minute
	// Bonus and penalty sizes for the confidence score.
	bonusGap, bonusFall, bonusRecovery, bonusFlat, bonusRecurrence = 0.15, 0.15, 0.15, 0.10, 0.15
	penaltyActivity, penaltySlowDecline                            = 0.25, 0.25
	// artifactRecurrenceHours is how close, in hours of the day, two
	// compression lows on different nights must start to count as recurring.
	artifactRecurrenceHours = 1.5
	// artifactRecurrenceOthers is how many other nights must show a similar
	// low for the recurrence bonus.
	artifactRecurrenceOthers = 2
	// artifactSlowDecline is the fall over the half hour before a low that
	// counts as a slow decline, in mg/dL.
	artifactSlowDecline = 20.0

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
	// Windows are activity windows. A real fast fall happens during
	// exercise, so a span overlapping one gets a lower confidence.
	Windows []Window
}

// Artifact is one suspected sensor artifact.
type Artifact struct {
	Kind ArtifactKind `json:"kind"`
	// Start and End are the first and last affected reading; ExcludeArtifacts
	// removes readings inside [Start, End].
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// Reason is a one-line explanation of what was matched.
	Reason string `json:"reason"`
	// Reasons lists every factor behind the confidence, for a tooltip:
	// what supports the call and what makes it doubtful.
	Reasons    []string `json:"reasons,omitempty"`
	Confidence float64  `json:"confidence"` // 0 to 1
	Nadir      float64  `json:"nadir"`      // lowest reading in the span, mg/dL
	PreLevel   float64  `json:"preLevel"`   // the level before it, mg/dL
	PostLevel  float64  `json:"postLevel"`  // the first reading after it, mg/dL
	// Marked is true when a person confirmed the span by hand.
	Marked bool `json:"marked,omitempty"`
}

// Detail is Reasons as one string, for a tooltip.
func (a Artifact) Detail() string {
	if len(a.Reasons) == 0 {
		return a.Reason
	}
	return a.Reason + ": " + strings.Join(a.Reasons, "; ")
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

func median(v []float64) float64 {
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

func overlapsWindow(ws []Window, from, to time.Time) bool {
	for _, w := range ws {
		if !w.End.Before(from) && !w.Start.After(to) {
			return true
		}
	}
	return false
}

// DetectArtifacts finds suspected sensor artifacts, ordered by Start.
//
// A compression low is a run of readings below thr.Low that starts in the
// local night, lasts at most CompressionMaxDuration, falls in at
// CompressionMinRate or faster from a reading no more than 15 minutes
// before, and recovers at that rate or faster (or returns to the prior level
// across a signal gap). Lows that recur near the same hour on several nights
// gain confidence; a slow decline beforehand loses it.
//
// A dip is a fall of at least DipMinDrop below the baseline (the median of
// the readings before it) within two readings, followed by a flat stretch of
// at most DipMaxStayReadings readings that stays well below the baseline, and
// a return to within DipRecoveryTolerance of the baseline over at most
// DipMaxRamp readings. It is looked for at any time of day and at any level,
// and only where no compression low was found.
//
// A real low falls more slowly, stays longer, drifts, or climbs back slowly,
// and is left alone. Upward jumps are never flagged.
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

	out := detectCompression(s, thr, loc, from, to, o.Windows)
	for _, d := range detectDips(s, o.Windows) {
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

// score turns a base confidence and lists of support and doubts into the
// final confidence and the reasons.
type score struct {
	conf    float64
	reasons []string
}

func (sc *score) plus(v float64, why string) {
	sc.conf += v
	sc.reasons = append(sc.reasons, why)
}

func (sc *score) minus(v float64, why string) {
	sc.conf -= v
	sc.reasons = append(sc.reasons, why)
}

func (sc *score) final() float64 { return math.Max(0, math.Min(sc.conf, 1)) }

func detectCompression(s []stats.Sample, thr Thresholds, loc *time.Location, nightFrom, nightTo int, windows []Window) []Artifact {
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
		dur := s[end].Time.Sub(s[start].Time)
		sc := score{conf: 0.5}
		sc.reasons = append(sc.reasons, fmt.Sprintf("fell %.1f mg/dL per min", onset))
		if onset >= artifactFastFall {
			sc.plus(bonusFall, "faster than interstitial glucose usually falls")
		}
		if rec >= artifactFastFall {
			sc.plus(bonusRecovery, "recovered fast")
		}
		if dur <= 45*time.Minute {
			sc.plus(0.1, "short")
		}
		if gapBefore || gapAfter {
			sc.plus(bonusGap, "signal gap next to the low")
		}
		// A slow decline before the low suggests a real nocturnal low.
		if back := valueBefore(s, start, 30*time.Minute); !math.IsNaN(back) && back-pre >= artifactSlowDecline {
			sc.minus(penaltySlowDecline, fmt.Sprintf("declined %.0f mg/dL over the half hour before (real lows usually decline gradually)", back-pre))
		}
		if overlapsWindow(windows, s[start].Time, s[end].Time) {
			sc.minus(penaltyActivity, "during an activity (real fast falls happen in exercise)")
		}
		out = append(out, Artifact{
			Kind: ArtifactCompression, Start: s[start].Time, End: s[end].Time,
			Reason:  fmt.Sprintf("fast fall and fast recovery, %d min, overnight", int(dur.Minutes())),
			Reasons: sc.reasons, Confidence: sc.final(), Nadir: nadir, PreLevel: pre, PostLevel: post,
		})
	}
	applyRecurrence(out, loc)
	return out
}

// valueBefore is the reading at least back before s[i], taken from the
// nearest earlier reading no more than a further 15 minutes older; NaN when
// there is none.
func valueBefore(s []stats.Sample, i int, back time.Duration) float64 {
	limit := s[i].Time.Add(-back)
	for k := i - 1; k >= 0; k-- {
		if s[k].Time.After(limit) {
			continue
		}
		if limit.Sub(s[k].Time) <= 15*time.Minute {
			return s[k].Value
		}
		break
	}
	return math.NaN()
}

// applyRecurrence adds confidence to compression lows that start near the
// same local hour on several different nights: a habit of the sensor or the
// sleeping position, not of the body.
func applyRecurrence(arts []Artifact, loc *time.Location) {
	if len(arts) <= artifactRecurrenceOthers {
		return
	}
	hourOf := func(t time.Time) float64 {
		l := t.In(loc)
		return float64(l.Hour()) + float64(l.Minute())/60
	}
	dayOf := func(t time.Time) string { return t.In(loc).Format("2006-01-02") }
	for i := range arts {
		nights := map[string]bool{}
		for j := range arts {
			if i == j || dayOf(arts[j].Start) == dayOf(arts[i].Start) {
				continue
			}
			d := math.Abs(hourOf(arts[i].Start) - hourOf(arts[j].Start))
			if d > 12 {
				d = 24 - d
			}
			if d <= artifactRecurrenceHours {
				nights[dayOf(arts[j].Start)] = true
			}
		}
		if len(nights) >= artifactRecurrenceOthers {
			arts[i].Confidence = math.Min(1, arts[i].Confidence+bonusRecurrence)
			arts[i].Reasons = append(arts[i].Reasons, fmt.Sprintf("similar low near this hour on %d other nights", len(nights)))
		}
	}
}

func detectDips(s []stats.Sample, windows []Window) []Artifact {
	var out []Artifact
	n := len(s)
	for j := 1; j < n; j++ {
		// The baseline is the median of the readings from DipBaselineSpan
		// before the fall window; without two of them nothing can be judged.
		hi := s[j].Time.Add(-DipDropWindow)
		lo := hi.Add(-DipBaselineSpan)
		var base []float64
		for x := j - 1; x >= 0 && !s[x].Time.Before(lo); x-- {
			if s[x].Time.Before(hi) {
				base = append(base, s[x].Value)
			}
		}
		if len(base) < 2 {
			continue
		}
		baseline := median(base)
		drop := baseline - s[j].Value
		if drop < DipMinDrop {
			continue
		}
		// The fall must be abrupt: a reading near the baseline no more than
		// two readings earlier, and no earlier reading already depressed
		// (that one would have started the dip).
		last := -1
		abrupt := true
		for x := j - 1; x >= 0 && s[j].Time.Sub(s[x].Time) <= DipDropWindow; x-- {
			if baseline-s[x].Value >= DipMinDrop {
				abrupt = false
				break
			}
			if last < 0 && math.Abs(baseline-s[x].Value) <= dipBaselineNoise {
				last = x
			}
		}
		if !abrupt || last < 0 {
			continue
		}

		// The stay: consecutive readings that stay well below the baseline
		// and roughly level with the first one.
		e := j
		for e+1 < n {
			nx := s[e+1]
			if nx.Time.Sub(s[e].Time) > artifactStepGap || nx.Time.Sub(s[j].Time) > DipMaxStay {
				break
			}
			if baseline-nx.Value < DipStayFraction*drop || math.Abs(nx.Value-s[j].Value) > DipPlateauBand {
				break
			}
			e++
		}
		if e-j+1 > DipMaxStayReadings {
			continue
		}
		// The recovery: back within tolerance of the baseline after at most
		// DipMaxRamp in-between readings that climb.
		k := -1
		for x := e + 1; x < n && x <= e+1+DipMaxRamp; x++ {
			if s[x].Time.Sub(s[e].Time) > dipMaxRecoveryGap {
				break
			}
			if s[x].Value >= baseline-DipRecoveryTolerance {
				k = x
				break
			}
			if s[x].Value < s[x-1].Value-5 {
				break // still falling: not a recovery
			}
		}
		if k < 0 {
			continue
		}
		if (s[k].Value-s[e].Value)/minutes(s[k].Time.Sub(s[e].Time)) < DipMinRecoveryRate {
			continue
		}
		nadir := s[j].Value
		for x := j; x < k; x++ {
			nadir = math.Min(nadir, s[x].Value)
		}
		fall := (s[last].Value - s[j].Value) / minutes(s[j].Time.Sub(s[last].Time))

		sc := score{conf: 0.5}
		sc.reasons = append(sc.reasons, fmt.Sprintf("dropped %.0f mg/dL below the earlier level", baseline-nadir))
		if hasGap(s, j, k) {
			sc.plus(bonusGap, "signal gap during the dip")
		}
		if fall >= artifactFastFall {
			sc.plus(bonusFall, fmt.Sprintf("fell %.1f mg/dL per min, faster than interstitial glucose usually falls", fall))
		}
		if math.Abs(s[k].Value-baseline) <= 10 {
			sc.plus(bonusRecovery, "returned to within 10 mg/dL of the earlier level")
		}
		if spread(base) <= 10 {
			sc.plus(bonusFlat, "flat before the drop")
		}
		if overlapsWindow(windows, s[j].Time, s[k-1].Time) {
			sc.minus(penaltyActivity, "during an activity (real fast falls happen in exercise)")
		}
		out = append(out, Artifact{
			Kind: ArtifactDip, Start: s[j].Time, End: s[k-1].Time,
			Reason: fmt.Sprintf("sudden drop of %.0f mg/dL for %d readings, then back to the earlier level",
				baseline-nadir, k-j),
			Reasons: sc.reasons, Confidence: sc.final(), Nadir: nadir, PreLevel: baseline, PostLevel: s[k].Value,
		})
		j = k - 1
	}
	return out
}

// hasGap reports a silence of artifactGapNote or more between readings j..k.
func hasGap(s []stats.Sample, j, k int) bool {
	for x := j; x < k; x++ {
		if s[x+1].Time.Sub(s[x].Time) >= artifactGapNote {
			return true
		}
	}
	return false
}

func spread(v []float64) float64 {
	lo, hi := v[0], v[0]
	for _, x := range v {
		lo, hi = math.Min(lo, x), math.Max(hi, x)
	}
	return hi - lo
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
