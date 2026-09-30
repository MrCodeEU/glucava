package web

import (
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
	tr := d.tr()
	one := func(ref *ActivityRef, label, arrow string) g.Node {
		if ref == nil {
			return Span(append(comp("button"), g.Attr("aria-disabled", "true"), g.Attr("data-disabled", ""),
				Class("pointer-events-none opacity-40"), g.Text(label))...)
		}
		name := ref.Name
		if name == "" {
			name = tr.T("activity.untitled", "id", ref.ID)
		}
		return A(append(comp("button"), Href("/activity/"+ref.ID), g.Attr("rel", arrow),
			g.Attr("title", name+" · "+tr.Date(ref.Start.In(d.Loc), false)+" "+tr.Time(ref.Start.In(d.Loc))), g.Text(label))...)
	}
	return []g.Node{one(d.Prev, tr.T("activity.nav.older"), "prev"), one(d.Next, tr.T("activity.nav.newer"), "next")}
}

// activityChartCard is the interactive chart, with the old server-drawn SVG
// as a <noscript> fallback for browsers without JavaScript.
func activityChartCard(d ActivityData, thr analytics.Thresholds) g.Node {
	tr := d.tr()
	a := d.Act
	unit := render.Unit(d.Cfg.Unit)
	if len(d.Samples) == 0 {
		return Card(H2(g.Text(tr.T("activity.chart.title"))), EmptyState("chart", tr.T("activity.chart.empty"),
			tr.T("activity.chart.empty_hint")))
	}
	samples := append(d.Samples[:0:0], d.Samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i].Time.Before(samples[j].Time) })
	in := ActivityChartInput{
		Low: thr.Low, High: thr.High, VeryLow: thr.VeryLow, VeryHigh: thr.VeryHigh, Unit: unit,
		Start: a.Start.UnixMilli(), End: a.End().UnixMilli(), T: tr,
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
	return Card(H2(g.Text(tr.T("activity.chart.title"))),
		P(Class("muted"), g.Text(tr.T("activity.chart.help"))),
		Chart("activity-chart", ActivityChartOption(in), 400),
		g.El("noscript", GlucoseChart(ChartData{
			Samples: d.Samples, Unit: unit, Loc: d.Loc, T: tr, Start: a.Start, End: a.End(), Range: d.Cfg.Range(),
		})),
	)
}

// glucoseTiles are the numbers of the readings during the activity.
func glucoseTiles(d ActivityData, unit render.Unit) []g.Node {
	s := d.Act.Summary
	if s == nil {
		return nil
	}
	tr := d.tr()
	tiles := []g.Node{
		Tile(tr.T("kpi.tir"), pctT(tr, s.TIR, 0), tr.T("kpi.tir.sub", "below", tr.Num(s.Below, 0), "above", tr.Num(s.Above, 0))),
		Tile(tr.T("kpi.avg"), valueT(tr, s.Avg, unit), string(unit)),
		Tile(tr.T("activity.tile.minmax"), valueT(tr, s.Min, unit)+" / "+valueT(tr, s.Max, unit), string(unit)),
		Tile(tr.T("activity.tile.start_end"), valueT(tr, s.Start, unit)+" → "+valueT(tr, s.End, unit),
			signedGlucoseT(tr, s.End-s.Start, unit)+" "+string(unit)),
		Tile(tr.T("kpi.cv"), pctT(tr, s.CV, 0), tr.T("activity.tile.cv_sub", "sd", valueT(tr, s.StdDev, unit), "unit", string(unit))),
		Tile(tr.T("kpi.gmi"), pctT(tr, s.GMI, 1), tr.T("activity.tile.gmi_sub")),
		Tile(tr.T("activity.tile.extremes"), tr.Num(s.VeryLow, 0)+"% / "+tr.Num(s.VeryHigh, 0)+"%", tr.T("activity.tile.extremes_sub")),
		Tile(tr.T("activity.tile.readings"), tr.Int(s.Count), tr.T("activity.tile.readings_sub")),
	}
	if in := d.Insight; in != nil {
		tiles = append(tiles, Tile(tr.T("activity.tile.drop"), tr.Num(in.DropRate*unitScale(unit), 1),
			tr.T("activity.tile.drop_sub", "unit", string(unit))))
	}
	return tiles
}

// activityStatTiles are the numbers of the activity itself.
func activityStatTiles(d ActivityData) []g.Node {
	tr := d.tr()
	a := d.Act
	var tiles []g.Node
	if a.Duration > 0 {
		tiles = append(tiles, Tile(tr.T("activity.tile.duration"), fmtDurationT(tr, a.Duration), orDash(a.Sport)))
	}
	if a.Distance > 0 {
		sub := tr.T("activity.tile.no_pace")
		if pace := render.FormatPace(a.Sport, a.Distance, a.Duration); pace != "" {
			sub = pace
		}
		tiles = append(tiles, Tile(tr.T("activity.tile.distance"), tr.Num(a.Distance/1000, 2)+" km", sub))
	}
	if a.ElevationGain > 0 {
		tiles = append(tiles, Tile(tr.T("activity.tile.elevation"), tr.Num(a.ElevationGain, 0)+" m", tr.T("activity.tile.climbed")))
	}
	if h := d.HR; h != nil {
		tiles = append(tiles, Tile(tr.T("activity.tile.hr"), tr.Num(h.Avg, 0), tr.T("activity.tile.hr_sub", "max", tr.Num(h.Max, 0), "min", tr.Num(h.Min, 0))))
	}
	if r := d.Rank; r != nil {
		tiles = append(tiles, Tile(tr.T("activity.tile.rank", "sport", r.Sport), pctT(tr, r.Percent, 0),
			tr.Tn("activity.tile.rank_sub", r.Others, "sport", r.Sport)))
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
	tr := d.tr()
	cell := func(label string, has bool, v float64, sub string) g.Node {
		val := "–"
		if has {
			val = valueT(tr, v, unit)
		}
		return Div(Class("min-w-0"),
			Div(Class("text-xs font-semibold uppercase tracking-wide text-ink-2"), g.Text(label)),
			Div(Class("text-2xl font-bold tabular-nums"), g.Text(val), Span(Class("ml-1 text-sm font-normal text-ink-2"), g.Text(string(unit)))),
			Div(Class("text-sm text-ink-2"), g.Text(sub)))
	}
	var verdict g.Node
	switch {
	case in.PostLows > 0:
		verdict = Notice("warning", Strong(g.Text(tr.T("activity.around.low_title"))),
			g.Text(tr.T("activity.around.low_body", "v", valueT(tr, in.PostLowNadir, unit), "unit", string(unit))))
	case in.HasPost:
		verdict = P(Class("muted mb-0 text-sm"), g.Text(tr.T("activity.around.no_low")))
	}
	return Card(H2(g.Text(tr.T("activity.around.title"))),
		Div(Class("grid gap-4 sm:grid-cols-3"),
			cell(tr.T("activity.around.before"), in.HasPre, in.PreMean, tr.T("activity.around.average")),
			cell(tr.T("activity.around.during"), true, in.Avg, tr.T("activity.around.during_sub", "a", valueT(tr, in.StartGlucose, unit), "b", valueT(tr, in.EndGlucose, unit))),
			cell(tr.T("activity.around.after"), in.HasPost, in.PostMean, tr.T("activity.around.average")),
		),
		g.If(verdict != nil, Div(Class("mt-4"), verdict)),
	)
}

// ActivityBody is the part of the activity page that updates live.
func ActivityBody(d ActivityData) g.Node {
	tr := d.tr()
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
			Notice("error", Strong(g.Text(tr.T("activity.notice.failed"))), g.Text(a.Error))),
		g.If(a.Status == jobs.StatusPending, Notice("", Strong(g.Text(tr.T("activity.notice.queued"))), g.Text(tr.T("activity.notice.queued_body")))),
		g.If(a.Status == jobs.StatusProcessing && a.Error == "", Notice("", Strong(g.Text(tr.T("activity.notice.working"))), g.Text(processingText(d.Step)))),
		g.If(a.Status == jobs.StatusProcessing && a.Error != "", Notice("warning", Strong(g.Text(tr.T("activity.notice.unfinished"))), g.Text(a.Error))),
		g.If(len(d.Artifacts) > 0, artifactNotice(d)),
		activityChartCard(d, thr),
		g.If(len(gTiles) > 0, Grid("", gTiles...)),
		g.If(len(aTiles) > 0, Grid("", aTiles...)),
		g.If(d.Insight != nil, aroundCard(d, unit)),
		// The chart photo is square, so it sits beside the text cards instead
		// of stretching across the page.
		Grid(cols,
			g.If(d.Cfg.ChartImage, Card(H2(g.Text(tr.T("activity.photo.title"))),
				Img(Alt(tr.T("activity.photo.alt")), Src("/chart/"+a.StravaID+".png"),
					g.Attr("style", "display:block;width:100%;height:auto;border-radius:8px")),
				P(Class("muted"), g.Text(tr.T("activity.photo.note"))),
			)),
			Div(Class("stack"),
				Card(H2(g.Text(tr.T("activity.block.title"))),
					g.If(d.Block != "", Pre(g.Text(d.Block))),
					g.If(d.Block == "", P(Class("muted"), g.Text(tr.T("activity.block.empty")))),
					P(Class("muted"), g.Text(tr.T("activity.block.note")))),
				Card(H2(g.Text(tr.T("activity.processing.title"))),
					Dl(append(comp("dl"),
						Dt(g.Text(tr.T("activity.processing.status"))), Dd(StatusBadgeT(tr, a.Status)),
						Dt(g.Text(tr.T("activity.processing.attempts"))), Dd(g.Text(tr.Int(a.Attempts))),
						g.If(d.Cfg.ChartImage, Dt(g.Text(tr.T("activity.processing.chart_photo")))),
						g.If(d.Cfg.ChartImage, Dd(g.Text(map[bool]string{true: tr.T("activity.processing.sent_once"), false: tr.T("activity.processing.not_sent")}[a.ChartUploaded]))),
						g.If(d.Cfg.HRRead, Dt(g.Text(tr.T("activity.processing.hr")))),
						g.If(d.Cfg.HRRead, Dd(g.Text(map[bool]string{true: tr.T("activity.processing.hr_stored"), false: tr.T("activity.processing.hr_unread")}[len(a.HeartRate) > 0]))),
						Dt(g.Text(tr.T("activity.processing.strava_id"))), Dd(Code(g.Text(a.StravaID))),
					)...),
					eventList(d.Events, d.Loc, d.Now),
				),
			),
		),
	)
}
