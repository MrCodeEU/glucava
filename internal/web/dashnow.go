package web

import (
	"fmt"
	"sort"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// nowStaleAfter is how old the newest reading may be before the card says it
// is out of date instead of presenting it as "now".
const nowStaleAfter = 20 * time.Minute

// nowSamples are the last 3 hours of readings, oldest first.
func nowSamples(day []stats.Sample, now time.Time) []stats.Sample {
	cut := now.Add(-3 * time.Hour)
	var out []stats.Sample
	for _, s := range day {
		if !s.Time.Before(cut) && !s.Time.After(now) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// bandColour colours a value by the range it falls in.
func bandColour(v float64, thr analytics.Thresholds) string {
	switch thr.BandOf(v) {
	case analytics.BandVeryLow:
		return ColVeryLow
	case analytics.BandLow:
		return ColLow
	case analytics.BandHigh:
		return ColHigh
	case analytics.BandVeryHigh:
		return ColVeryHigh
	}
	return ColInRange
}

// nowCard is the "how am I doing right now" card: the current value with its
// direction, the last three hours against the target range, and the last 24
// hours as a five-band donut. It always renders the same element ids, so the
// live stream can morph it in place.
func nowCard(d DashData) g.Node {
	cur := d.Latest
	if cur == nil && len(d.Day) > 0 {
		last := d.Day[len(d.Day)-1]
		cur = &last
	}
	if cur == nil {
		return Card(ID("now"), H2(g.Text("Now")), EmptyState("chart", "No glucose readings yet",
			"The current value, the last hours and the 24 hour split show up here once a source delivers readings."))
	}
	thr := d.Thr
	age := d.Now.Sub(cur.Time)
	stale := age > nowStaleAfter

	valueStyle := "color:" + bandColour(cur.Value, thr)
	if stale {
		valueStyle = "opacity:.55"
	}
	arrow := ""
	trendText := ""
	if t := analytics.CurrentTrend(nowSamples(d.Day, d.Now)); t.OK && !stale {
		arrow = t.Arrow
		trendText = fmt.Sprintf("%s · %s %s/min", t.Label, signedGlucose(t.Rate, d.Unit), d.Unit)
	}
	sub := cur.Time.In(d.Loc).Format("15:04")
	if age >= 0 {
		sub += " · " + fmtDuration(age) + " ago"
	}

	left := Div(Class("min-w-0"),
		Div(Class("text-xs font-semibold uppercase tracking-wide text-ink-2"), g.Text("Current glucose")),
		Div(Class("flex items-baseline gap-2"),
			Span(Class("text-5xl font-bold leading-none tabular-nums"), g.Attr("style", valueStyle), g.Text(render.Value(cur.Value, d.Unit))),
			Span(Class("text-3xl font-semibold leading-none"), g.Attr("aria-label", trendText), g.Attr("title", trendText), g.Text(arrow)),
			Span(Class("text-sm text-ink-2"), g.Text(string(d.Unit))),
		),
		Div(Class("mt-1 text-sm text-ink-2"), g.Text(sub)),
		g.If(trendText != "", Div(Class("text-sm text-ink-2"), g.Text(trendText))),
		g.If(stale, Div(Class("mt-1 text-sm font-medium text-warn"), g.Text("Out of date: no newer reading yet"))),
	)

	recent := nowSamples(d.Day, d.Now)
	var mid g.Node
	if len(recent) >= 2 {
		pts := make([]TrendPoint, len(recent))
		for i, s := range recent {
			pts[i] = TrendPoint{T: s.Time.UnixMilli(), V: s.Value}
		}
		mid = Div(Class("min-w-0"),
			Div(Class("text-xs font-semibold uppercase tracking-wide text-ink-2"), g.Text("Last 3 hours")),
			Chart("dash-3h", TrendOption(TrendInput{
				Series: []TrendSeries{{Name: "Glucose (" + string(d.Unit) + ")", Color: colLine, Points: pts}},
				Low:    thr.Low, High: thr.High, Unit: d.Unit,
			}), 170))
	} else {
		mid = Div(Class("grid min-w-0 place-items-center text-sm text-ink-2"), g.Text("Not enough readings in the last 3 hours to draw."))
	}

	var right g.Node
	if d.DayTIR.Count > 0 {
		t := d.DayTIR
		right = Div(Class("min-w-0"),
			Div(Class("text-xs font-semibold uppercase tracking-wide text-ink-2"), g.Text("Last 24 hours")),
			Chart("dash-tir24", DonutOption(DonutInput{
				Slices: []DonutSlice{
					{Label: "Very low", Value: t.VeryLow, Color: ColVeryLow},
					{Label: "Low", Value: t.Low, Color: ColLow},
					{Label: "In range", Value: t.InRange, Color: ColInRange},
					{Label: "High", Value: t.High, Color: ColHigh},
					{Label: "Very high", Value: t.VeryHigh, Color: ColVeryHigh},
				},
				Centre: pct0(t.InRange), Sub: "in range",
			}), 170))
	} else {
		right = Div(Class("grid min-w-0 place-items-center text-sm text-ink-2"), g.Text("No readings in the last 24 hours."))
	}

	return Card(ID("now"),
		Div(Class("grid items-center gap-6 md:grid-cols-[minmax(11rem,14rem)_1fr_minmax(9rem,12rem)]"), left, mid, right))
}

// sportIcon picks an icon for an activity's sport.
func sportIcon(sport string) string {
	switch sport {
	case "Run", "TrailRun", "Walk", "Hike", "VirtualRun":
		return "footprints"
	case "Ride", "VirtualRide", "EBikeRide", "Handcycle", "MountainBikeRide", "GravelRide":
		return "bike"
	case "Swim":
		return "waves"
	}
	return "activity"
}
