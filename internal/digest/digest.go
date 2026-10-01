// Package digest builds the summary emails: one per processed activity and a
// weekly overview. It only builds and schedules messages; delivery is the
// caller's send function, so the same rules serve tests and the real mailer.
package digest

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/chartimg"
	"github.com/MrCodeEU/glucava/internal/i18n"
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

func pct(tr *i18n.Translator, v float64) string { return tr.T("email.pct", "v", tr.Num(v, 0)) }

// glucose formats a mg/dL value for unit, with the locale's decimal separator.
func glucose(tr *i18n.Translator, v float64, unit render.Unit) string {
	if unit == render.MmolL {
		return tr.Num(v/stats.MmolFactor, 1)
	}
	return tr.Num(v, 0)
}

func dur(tr *i18n.Translator, d time.Duration) string {
	d = d.Round(time.Minute)
	if h := int(d.Hours()); h > 0 {
		return tr.T("email.dur.hm", "h", fmt.Sprint(h), "m", fmt.Sprintf("%02d", int(d.Minutes())%60))
	}
	return tr.T("email.dur.min", "m", fmt.Sprint(int(d.Minutes())))
}

// day is a short date for a row label: "Mon 28 Sep" in English.
func day(tr *i18n.Translator, t time.Time) string {
	return tr.Weekday(t.Weekday(), false) + " " + tr.Date(t, false)
}

// ActivityMessage summarizes one processed activity. ok is false when the
// activity has no glucose summary to report. A glucose chart is added when
// samples are given; a chart that cannot be drawn is left out, never an error.
func ActivityMessage(tr *i18n.Translator, a jobs.Activity, unit render.Unit, rng stats.Range, samples []stats.Sample, loc *time.Location) (notify.Message, bool) {
	if a.Summary == nil {
		return notify.Message{}, false
	}
	s := a.Summary
	name := a.Name
	if name == "" {
		name = tr.T("email.activity.fallback_name", "id", a.StravaID)
	}
	facts := []notify.Fact{
		{Label: tr.T("email.fact.tir"), Value: pct(tr, s.TIR)},
		{Label: tr.T("email.fact.below"), Value: pct(tr, s.Below)},
		{Label: tr.T("email.fact.above"), Value: pct(tr, s.Above)},
		{Label: tr.T("email.fact.lowest"), Value: glucose(tr, s.Min, unit) + " " + string(unit)},
		{Label: tr.T("email.fact.highest"), Value: glucose(tr, s.Max, unit) + " " + string(unit)},
		{Label: tr.T("email.fact.average"), Value: glucose(tr, s.Avg, unit) + " " + string(unit)},
		{Label: tr.T("email.fact.startend"), Value: glucose(tr, s.Start, unit) + " → " + glucose(tr, s.End, unit)},
		{Label: tr.T("email.fact.duration"), Value: dur(tr, a.Duration)},
		{Label: tr.T("email.fact.readings"), Value: tr.Int(s.Count)},
	}
	if h, ok := stats.SummarizeHR(a.HeartRate, a.Start, a.End()); ok {
		facts = append(facts,
			notify.Fact{Label: tr.T("email.fact.hr"), Value: tr.T("email.fact.hr.value", "avg", tr.Num(h.Avg, 0), "max", tr.Num(h.Max, 0))})
	}
	when := tr.When(a.Start, loc, time.Now())
	body := tr.T("email.activity.body", "name", name, "when", when)
	if a.Sport != "" {
		body = tr.T("email.activity.body.sport", "name", name, "sport", a.Sport, "when", when)
	}
	sev := "info"
	if s.Below > 0 {
		sev = "warning"
	}
	m := notify.Message{
		Type: notify.TypeActivitySummary, Severity: sev, Title: tr.T("email.activity.title", "name", name),
		Body: body, StravaID: a.StravaID, Facts: facts, Time: time.Now(), Icon: sportIcon(a.Sport),
	}
	if len(samples) > 0 {
		png, err := chartimg.Glucose(chartimg.Series{Samples: samples, Range: rng, Start: a.Start, End: a.End(), Unit: unit, Loc: loc})
		if err != nil {
			slog.Error("digest chart", "activity", a.StravaID, "err", err)
		} else {
			m.Chart, m.ChartAlt = png, tr.T("email.activity.chart_alt")
		}
	}
	return m, true
}

// WeeklyMessage summarizes the activities that started in [from, to). prev
// are the activities of the week before, for the comparison. A week without
// processed activities gets a short note instead of numbers, so silence never
// has to be read as "glucava is broken". ok is always true.
func WeeklyMessage(tr *i18n.Translator, cur, prev []jobs.Activity, from, to time.Time, unit render.Unit, loc *time.Location) (notify.Message, bool) {
	cur = withSummary(cur)
	title := tr.T("email.weekly.title", "from", tr.Date(from.In(loc), false), "to", tr.Date(to.Add(-time.Second).In(loc), false))
	if len(cur) == 0 {
		return notify.Message{
			Type: notify.TypeWeeklySummary, Severity: "info", Title: title, Icon: "\U0001F634", // sleeping face
			Body: tr.T("email.weekly.empty"),
			Time: time.Now(),
		}, true
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
		return fmt.Sprintf("%s (%s)", n, day(tr, a.Start.In(loc)))
	}
	facts := []notify.Fact{
		{Label: tr.T("email.weekly.activities"), Value: tr.Int(len(cur))},
		{Label: tr.T("email.weekly.total_time"), Value: dur(tr, total)},
		{Label: tr.T("email.weekly.avg_tir"), Value: pct(tr, tir)},
	}
	if len(prev) > 0 {
		d := tir - meanTIR(prev)
		sign := "+"
		if d < 0 {
			sign, d = "-", -d
		}
		facts = append(facts, notify.Fact{Label: tr.T("email.weekly.change"), Value: tr.T("email.weekly.points", "v", sign+tr.Num(d, 0))})
	}
	facts = append(facts,
		notify.Fact{Label: tr.T("email.weekly.best"), Value: pct(tr, best.Summary.TIR) + " · " + label(best)},
		notify.Fact{Label: tr.T("email.weekly.worst"), Value: pct(tr, worst.Summary.TIR) + " · " + label(worst)},
		notify.Fact{Label: tr.T("email.weekly.lowest"), Value: glucose(tr, lowest.Summary.Min, unit) + " " + string(unit) + " · " + label(lowest)},
		notify.Fact{Label: tr.T("email.weekly.with_low"), Value: tr.T("email.weekly.of", "n", tr.Int(hypo), "total", tr.Int(len(cur)))},
	)
	sev := "info"
	if hypo > 0 {
		sev = "warning"
	}
	var bars []chartimg.Bar
	for _, a := range cur {
		bars = append(bars, chartimg.Bar{Label: barLabel(tr, a, loc), Below: a.Summary.Below, InRange: a.Summary.TIR, Above: a.Summary.Above})
	}
	chart, cerr := chartimg.Bars(bars)
	if cerr != nil {
		slog.Error("digest weekly chart", "err", cerr)
	}
	return notify.Message{
		Type: notify.TypeWeeklySummary, Severity: sev,
		Title: title,
		Body:  tr.T("email.weekly.body"), Facts: facts, Time: time.Now(),
		Chart: chart, ChartAlt: tr.T("email.weekly.chart_alt"),
	}, true
}

// barLabel is the short row label of the weekly chart: the day and the name.
func barLabel(tr *i18n.Translator, a jobs.Activity, loc *time.Location) string {
	n := a.Name
	if n == "" {
		n = a.StravaID
	}
	st := a.Start.In(loc)
	return tr.Weekday(st.Weekday(), false) + " " + fmt.Sprint(st.Day()) + ": " + n
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

// Weekly sends the weekly summary (or a "no activities" note) once per week, on Monday from 08:00 (or
// later that week if the server was down), when enabled.
type Weekly struct {
	Store   Store
	Enabled func() bool
	Unit    func() render.Unit
	Loc     func() *time.Location
	Send    func(ctx context.Context, m notify.Message) error
	Now     func() time.Time
	Every   time.Duration // check interval; default 15 minutes
	// Tr gives the installation language at send time; nil means English.
	Tr notify.Translator
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

	msg, ok := WeeklyMessage(w.Tr.Get(), cur, prev, from, monday, w.Unit(), loc)
	if err := w.Send(ctx, msg); err != nil {
		return false, err
	}
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
			slog.Error("digest weekly summary", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
