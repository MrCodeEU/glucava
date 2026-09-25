// Package digest builds the summary emails: one per processed activity and a
// weekly overview. It only builds and schedules messages; delivery is the
// caller's send function, so the same rules serve tests and the real mailer.
package digest

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/chartimg"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// sportIcon picks an emoji for a Strava sport type; unknown sports get a medal.
func sportIcon(sport string) string {
	switch strings.ToLower(sport) {
	case "run", "trailrun", "virtualrun":
		return "\U0001F3C3" // runner
	case "ride", "virtualride", "gravelride", "mountainbikeride", "ebikeride", "handcycle":
		return "\U0001F6B4" // cyclist
	case "swim":
		return "\U0001F3CA" // swimmer
	case "walk":
		return "\U0001F6B6" // walker
	case "hike":
		return "\U0001F97E" // hiking boot
	case "nordicski", "alpineski", "backcountryski":
		return "\u26F7\uFE0F" // skier
	case "weighttraining", "workout", "crossfit":
		return "\U0001F3CB" // weightlifter
	case "yoga":
		return "\U0001F9D8" // person in lotus position
	}
	return "\U0001F3C5" // medal
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", v) }

func dur(d time.Duration) string {
	d = d.Round(time.Minute)
	if h := int(d.Hours()); h > 0 {
		return fmt.Sprintf("%dh %02dm", h, int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// ActivityMessage summarizes one processed activity. ok is false when the
// activity has no glucose summary to report. A glucose chart is added when
// samples are given; a chart that cannot be drawn is left out, never an error.
func ActivityMessage(a jobs.Activity, unit render.Unit, rng stats.Range, samples []stats.Sample, loc *time.Location) (notify.Message, bool) {
	if a.Summary == nil {
		return notify.Message{}, false
	}
	s := a.Summary
	name := a.Name
	if name == "" {
		name = "Activity " + a.StravaID
	}
	facts := []notify.Fact{
		{Label: "Time in range", Value: pct(s.TIR)},
		{Label: "Below range", Value: pct(s.Below)},
		{Label: "Above range", Value: pct(s.Above)},
		{Label: "Lowest", Value: render.Value(s.Min, unit) + " " + string(unit)},
		{Label: "Highest", Value: render.Value(s.Max, unit) + " " + string(unit)},
		{Label: "Average", Value: render.Value(s.Avg, unit) + " " + string(unit)},
		{Label: "At start / at end", Value: render.Value(s.Start, unit) + " → " + render.Value(s.End, unit)},
		{Label: "Duration", Value: dur(a.Duration)},
		{Label: "Readings", Value: fmt.Sprint(s.Count)},
	}
	body := name
	if a.Sport != "" {
		body += " (" + a.Sport + ")"
	}
	body += " on " + a.Start.In(loc).Format("Mon 2 Jan, 15:04") + " was annotated on Strava."
	sev := "info"
	if s.Below > 0 {
		sev = "warning"
	}
	m := notify.Message{
		Type: notify.TypeActivitySummary, Severity: sev, Title: "Activity summary: " + name,
		Body: body, StravaID: a.StravaID, Facts: facts, Time: time.Now(), Icon: sportIcon(a.Sport),
	}
	if len(samples) > 0 {
		png, err := chartimg.Glucose(chartimg.Series{Samples: samples, Range: rng, Start: a.Start, End: a.End(), Unit: unit, Loc: loc})
		if err != nil {
			log.Printf("digest: chart for %s: %v", a.StravaID, err)
		} else {
			m.Chart, m.ChartAlt = png, "Glucose during the activity"
		}
	}
	return m, true
}

// WeeklyMessage summarizes the activities that started in [from, to). prev
// are the activities of the week before, for the comparison. ok is false when
// the week had no processed activity.
func WeeklyMessage(cur, prev []jobs.Activity, from, to time.Time, unit render.Unit, loc *time.Location) (notify.Message, bool) {
	cur = withSummary(cur)
	if len(cur) == 0 {
		return notify.Message{}, false
	}
	prev = withSummary(prev)

	var total time.Duration
	var worst, best jobs.Activity
	lowest := cur[0]
	hypo := 0
	for i, a := range cur {
		total += a.Duration
		if i == 0 || a.Summary.TIR < worst.Summary.TIR {
			worst = a
		}
		if i == 0 || a.Summary.TIR > best.Summary.TIR {
			best = a
		}
		if a.Summary.Min < lowest.Summary.Min {
			lowest = a
		}
		if a.Summary.Below > 0 {
			hypo++
		}
	}
	tir := meanTIR(cur)
	label := func(a jobs.Activity) string {
		n := a.Name
		if n == "" {
			n = a.StravaID
		}
		return fmt.Sprintf("%s (%s)", n, a.Start.In(loc).Format("Mon 2 Jan"))
	}
	facts := []notify.Fact{
		{Label: "Activities", Value: fmt.Sprint(len(cur))},
		{Label: "Total time", Value: dur(total)},
		{Label: "Average time in range", Value: pct(tir)},
	}
	if len(prev) > 0 {
		d := tir - meanTIR(prev)
		facts = append(facts, notify.Fact{Label: "Change vs the week before", Value: fmt.Sprintf("%+.0f points", d)})
	}
	facts = append(facts,
		notify.Fact{Label: "Best time in range", Value: pct(best.Summary.TIR) + " · " + label(best)},
		notify.Fact{Label: "Lowest time in range", Value: pct(worst.Summary.TIR) + " · " + label(worst)},
		notify.Fact{Label: "Lowest glucose", Value: render.Value(lowest.Summary.Min, unit) + " " + string(unit) + " · " + label(lowest)},
		notify.Fact{Label: "Activities with a low", Value: fmt.Sprintf("%d of %d", hypo, len(cur))},
	)
	sev := "info"
	if hypo > 0 {
		sev = "warning"
	}
	var bars []chartimg.Bar
	for _, a := range cur {
		bars = append(bars, chartimg.Bar{Label: label(a), Below: a.Summary.Below, InRange: a.Summary.TIR, Above: a.Summary.Above})
	}
	chart, cerr := chartimg.Bars(bars)
	if cerr != nil {
		log.Printf("digest: weekly chart: %v", cerr)
	}
	last := to.Add(-time.Second)
	return notify.Message{
		Type: notify.TypeWeeklySummary, Severity: sev,
		Title: fmt.Sprintf("Weekly summary, %s – %s", from.In(loc).Format("2 Jan"), last.In(loc).Format("2 Jan")),
		Body:  "Your glucose numbers for last week's activities.", Facts: facts, Time: time.Now(),
		Chart: chart, ChartAlt: "Time in range per activity: green in range, red below, orange above",
	}, true
}

func withSummary(in []jobs.Activity) []jobs.Activity {
	var out []jobs.Activity
	for _, a := range in {
		if a.Summary != nil && a.Status == jobs.StatusDone {
			out = append(out, a)
		}
	}
	return out
}

// meanTIR weights each activity by its number of readings.
func meanTIR(acts []jobs.Activity) float64 {
	var sum, n float64
	for _, a := range acts {
		sum += a.Summary.TIR * float64(a.Summary.Count)
		n += float64(a.Summary.Count)
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// WeekStart returns Monday 08:00 of the week containing t, in loc.
func WeekStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	back := (int(t.Weekday()) + 6) % 7 // days since Monday
	d := t.AddDate(0, 0, -back)
	return time.Date(d.Year(), d.Month(), d.Day(), 8, 0, 0, 0, loc)
}

// Store is what the weekly scheduler needs.
type Store interface {
	ListActivities(ctx context.Context, limit int) ([]jobs.Activity, error)
	WeeklyLast() time.Time
	SetWeeklyLast(t time.Time) error
}

// Weekly sends the weekly summary once per week, on Monday from 08:00 (or
// later that week if the server was down), when enabled.
type Weekly struct {
	Store   Store
	Enabled func() bool
	Unit    func() render.Unit
	Loc     func() *time.Location
	Send    func(ctx context.Context, m notify.Message) error
	Now     func() time.Time
	Every   time.Duration // check interval; default 15 minutes
}

func (w *Weekly) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Once sends the summary if it is due. It returns whether a mail went out.
func (w *Weekly) Once(ctx context.Context) (bool, error) {
	if !w.Enabled() {
		return false, nil
	}
	loc := w.Loc()
	now := w.now()
	due := WeekStart(now, loc)
	if now.Before(due) || !w.Store.WeeklyLast().Before(due) {
		return false, nil
	}
	acts, err := w.Store.ListActivities(ctx, 500)
	if err != nil {
		return false, err
	}
	// The report covers Monday to Sunday of the week before this one.
	monday := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, loc)
	from, prevFrom := monday.AddDate(0, 0, -7), monday.AddDate(0, 0, -14)
	var cur, prev []jobs.Activity
	for _, a := range acts {
		switch {
		case !a.Start.Before(from) && a.Start.Before(monday):
			cur = append(cur, a)
		case !a.Start.Before(prevFrom) && a.Start.Before(from):
			prev = append(prev, a)
		}
	}
	sort.Slice(cur, func(i, j int) bool { return cur[i].Start.Before(cur[j].Start) })

	msg, ok := WeeklyMessage(cur, prev, from, monday, w.Unit(), loc)
	if ok {
		if err := w.Send(ctx, msg); err != nil {
			return false, err
		}
	}
	// A quiet week is recorded too, so it is not re-checked every 15 minutes.
	return ok, w.Store.SetWeeklyLast(now)
}

// Run checks until ctx is cancelled.
func (w *Weekly) Run(ctx context.Context) {
	every := w.Every
	if every <= 0 {
		every = 15 * time.Minute
	}
	for {
		if _, err := w.Once(ctx); err != nil {
			log.Printf("digest: weekly summary: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
