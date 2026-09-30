package web

import (
	"fmt"
	"sort"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/render"
)

// activityNav is the previous/next pair in the page header. A missing
// neighbour renders a disabled button so the header does not jump around.
func activityNav(d ActivityData) []g.Node {
	one := func(ref *ActivityRef, label, arrow string) g.Node {
		if ref == nil {
			return Span(append(comp("button"), g.Attr("aria-disabled", "true"), g.Attr("data-disabled", ""),
				Class("pointer-events-none opacity-40"), g.Text(label))...)
		}
		name := ref.Name
		if name == "" {
			name = "Activity " + ref.ID
		}
		return A(append(comp("button"), Href("/activity/"+ref.ID), g.Attr("rel", arrow),
			g.Attr("title", name+" · "+ref.Start.In(d.Loc).Format("2 Jan 15:04")), g.Text(label))...)
	}
	return []g.Node{one(d.Prev, "← Older", "prev"), one(d.Next, "Newer →", "next")}
}

// activityChartCard is the interactive chart, with the old server-drawn SVG
// as a <noscript> fallback for browsers without JavaScript.
func activityChartCard(d ActivityData, thr analytics.Thresholds) g.Node {
	a := d.Act
	unit := render.Unit(d.Cfg.Unit)
	if len(d.Samples) == 0 {
		return Card(H2(g.Text("Glucose")), EmptyState("chart", "No glucose readings for this activity",
			"Readings appear here once the source has data for the activity's time."))
	}
	samples := append(d.Samples[:0:0], d.Samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i].Time.Before(samples[j].Time) })
	in := ActivityChartInput{
		Low: thr.Low, High: thr.High, VeryLow: thr.VeryLow, VeryHigh: thr.VeryHigh, Unit: unit,
		Start: a.Start.UnixMilli(), End: a.End().UnixMilli(),
	}
	if d.Cfg.PreMin > 0 || d.Cfg.PostMin > 0 {
		in.From = a.Start.UnixMilli() - int64(d.Cfg.PreMin)*60_000
		in.To = a.End().UnixMilli() + int64(d.Cfg.PostMin)*60_000
	}
	for _, s := range samples {
		in.Glucose = append(in.Glucose, TrendPoint{T: s.Time.UnixMilli(), V: s.Value})
	}
	for _, h := range a.HeartRate {
		in.HR = append(in.HR, TrendPoint{T: h.Time.UnixMilli(), V: h.BPM})
	}
	for _, e := range a.Elevation {
		in.Elevation = append(in.Elevation, TrendPoint{T: e.Time.UnixMilli(), V: e.Meters})
	}
	return Card(H2(g.Text("Glucose")),
		P(Class("muted"), g.Text("Drag the slider or scroll to zoom; hover for exact values. The shaded spans are the activity and the before/after window.")),
		Chart("activity-chart", ActivityChartOption(in), 400),
		g.El("noscript", GlucoseChart(ChartData{
			Samples: d.Samples, Unit: unit, Loc: d.Loc, Start: a.Start, End: a.End(), Range: d.Cfg.Range(),
		})),
	)
}

// glucoseTiles are the numbers of the readings during the activity.
func glucoseTiles(d ActivityData, unit render.Unit) []g.Node {
	s := d.Act.Summary
	if s == nil {
		return nil
	}
	tiles := []g.Node{
		Tile("Time in range", fmt.Sprintf("%.0f%%", s.TIR), fmt.Sprintf("%.0f%% below · %.0f%% above", s.Below, s.Above)),
		Tile("Average", render.Value(s.Avg, unit), string(unit)),
		Tile("Min / Max", render.Value(s.Min, unit)+" / "+render.Value(s.Max, unit), string(unit)),
		Tile("Start → end", render.Value(s.Start, unit)+" → "+render.Value(s.End, unit),
			signedGlucose(s.End-s.Start, unit)+" "+string(unit)),
		Tile("Variability (CV)", fmt.Sprintf("%.0f%%", s.CV), fmt.Sprintf("target under 36%% · SD %s %s", render.Value(s.StdDev, unit), unit)),
		Tile("GMI", fmt.Sprintf("%.1f%%", s.GMI), "estimated A1C from this window"),
		Tile("Very low / very high", fmt.Sprintf("%.0f%% / %.0f%%", s.VeryLow, s.VeryHigh), "of the readings"),
		Tile("Readings", fmt.Sprint(s.Count), "in the activity window"),
	}
	if in := d.Insight; in != nil {
		tiles = append(tiles, Tile("Drop rate", fmt.Sprintf("%.1f", in.DropRate*unitScale(unit)),
			string(unit)+" per 10 min, positive = falling"))
	}
	return tiles
}

// activityStatTiles are the numbers of the activity itself.
func activityStatTiles(d ActivityData) []g.Node {
	a := d.Act
	var tiles []g.Node
	if a.Duration > 0 {
		tiles = append(tiles, Tile("Duration", fmtDuration(a.Duration), orDash(a.Sport)))
	}
	if a.Distance > 0 {
		sub := "no pace for this sport"
		if pace := render.FormatPace(a.Sport, a.Distance, a.Duration); pace != "" {
			sub = pace
		}
		tiles = append(tiles, Tile("Distance", fmt.Sprintf("%.2f km", a.Distance/1000), sub))
	}
	if a.ElevationGain > 0 {
		tiles = append(tiles, Tile("Elevation gain", fmt.Sprintf("%.0f m", a.ElevationGain), "climbed"))
	}
	if h := d.HR; h != nil {
		tiles = append(tiles, Tile("Heart rate", fmt.Sprintf("%.0f", h.Avg), fmt.Sprintf("bpm average · max %.0f · min %.0f", h.Max, h.Min)))
	}
	if r := d.Rank; r != nil {
		tiles = append(tiles, Tile("Rank among "+r.Sport, fmt.Sprintf("%.0f%%", r.Percent),
			fmt.Sprintf("time in range beats this share of %d other %s activities", r.Others, r.Sport)))
	}
	return tiles
}

// aroundCard compares glucose before, during and after the activity and
// flags a low that followed it.
func aroundCard(d ActivityData, unit render.Unit) g.Node {
	in := d.Insight
	if in == nil {
		return nil
	}
	cell := func(label string, has bool, v float64, sub string) g.Node {
		val := "–"
		if has {
			val = render.Value(v, unit)
		}
		return Div(Class("min-w-0"),
			Div(Class("text-xs font-semibold uppercase tracking-wide text-ink-2"), g.Text(label)),
			Div(Class("text-2xl font-bold tabular-nums"), g.Text(val), Span(Class("ml-1 text-sm font-normal text-ink-2"), g.Text(string(unit)))),
			Div(Class("text-sm text-ink-2"), g.Text(sub)))
	}
	var verdict g.Node
	switch {
	case in.PostLows > 0:
		verdict = Notice("warning", Strong(g.Text("A low followed. ")),
			g.Textf("Glucose dropped below range within 3 hours after this activity, down to %s %s.", render.Value(in.PostLowNadir, unit), unit))
	case in.HasPost:
		verdict = P(Class("muted mb-0 text-sm"), g.Text("No low in the 3 hours after this activity."))
	}
	return Card(H2(g.Text("Before, during and after")),
		Div(Class("grid gap-4 sm:grid-cols-3"),
			cell("Before (30 min)", in.HasPre, in.PreMean, "average"),
			cell("During", true, in.Avg, fmt.Sprintf("average · %s → %s", render.Value(in.StartGlucose, unit), render.Value(in.EndGlucose, unit))),
			cell("After (60 min)", in.HasPost, in.PostMean, "average"),
		),
		g.If(verdict != nil, Div(Class("mt-4"), verdict)),
	)
}

// ActivityBody is the part of the activity page that updates live.
func ActivityBody(d ActivityData) g.Node {
	a := d.Act
	unit := render.Unit(d.Cfg.Unit)
	thr := d.Thr
	if thr == (analytics.Thresholds{}) {
		thr = analytics.FromRange(d.Cfg.Range())
	}

	gTiles, aTiles := glucoseTiles(d, unit), activityStatTiles(d)
	cols := "2"
	if d.Cfg.ChartImage {
		cols = "photo"
	}
	return Div(ID("activity-body"),
		g.If(a.Status == jobs.StatusFailed && a.Error != "",
			Notice("error", Strong(g.Text("This activity failed. ")), g.Text(a.Error))),
		g.If(a.Status == jobs.StatusPending, Notice("", Strong(g.Text("Waiting in the queue. ")), g.Text("Jobs run one at a time; this page updates by itself."))),
		g.If(a.Status == jobs.StatusProcessing && a.Error == "", Notice("", Strong(g.Text("Working on it. ")), g.Text(processingText(d.Step)))),
		g.If(a.Status == jobs.StatusProcessing && a.Error != "", Notice("warning", Strong(g.Text("Not finished yet. ")), g.Text(a.Error))),
		g.If(len(d.Artifacts) > 0, artifactNotice(d)),
		activityChartCard(d, thr),
		g.If(len(gTiles) > 0, Grid("", gTiles...)),
		g.If(len(aTiles) > 0, Grid("", aTiles...)),
		g.If(d.Insight != nil, aroundCard(d, unit)),
		// The chart photo is square, so it sits beside the text cards instead
		// of stretching across the page.
		Grid(cols,
			g.If(d.Cfg.ChartImage, Card(H2(g.Text("Chart photo for Strava")),
				Img(Alt("Glucose chart as attached to Strava"), Src("/chart/"+a.StravaID+".png"),
					g.Attr("style", "display:block;width:100%;height:auto;border-radius:8px")),
				P(Class("muted"), g.Text("What glucava attaches, with your current chart settings.")),
			)),
			Div(Class("stack"),
				Card(H2(g.Text("Strava description block")),
					g.If(d.Block != "", Pre(g.Text(d.Block))),
					g.If(d.Block == "", P(Class("muted"), g.Text("Nothing to show yet: no glucose readings are stored for this activity."))),
					P(Class("muted"), g.Text("This block is added to the description on Strava. Your own text there is kept."))),
				Card(H2(g.Text("Processing")),
					Dl(append(comp("dl"),
						Dt(g.Text("Status")), Dd(StatusBadge(a.Status)),
						Dt(g.Text("Attempts")), Dd(g.Textf("%d", a.Attempts)),
						g.If(d.Cfg.ChartImage, Dt(g.Text("Chart photo"))),
						g.If(d.Cfg.ChartImage, Dd(g.Text(map[bool]string{true: "sent once", false: "not sent"}[a.ChartUploaded]))),
						g.If(d.Cfg.HRRead, Dt(g.Text("Heart rate"))),
						g.If(d.Cfg.HRRead, Dd(g.Text(map[bool]string{true: "stored", false: "not read yet"}[len(a.HeartRate) > 0]))),
						Dt(g.Text("Strava ID")), Dd(Code(g.Text(a.StravaID))),
					)...),
					eventList(d.Events, d.Loc, d.Now),
				),
			),
		),
	)
}
