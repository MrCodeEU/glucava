package web

import (
	"fmt"
	"sort"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
)

// This file renders the Overview cards. Each card is a function of
// StatsData; overviewCardRenderers maps a card id (store.OverviewCards) to
// its function, and StatsPage draws the configured layout with them. The
// registry test checks that every id has a renderer.

// StatsData feeds the Overview page.
type StatsData struct {
	Range   statsRange
	Model   *overviewModel
	Cards   []store.OverviewCard // the layout, in order
	Sources []store.SourceInfo
	Unit    render.Unit
	Thr     analytics.Thresholds // mg/dL
	Loc     *time.Location
	Now     time.Time
	T       *i18n.Translator // set by StatsPage from the page's translator; nil means English
}

// cardRenderer draws one card; opts are the card's configured options.
type cardRenderer func(d StatsData, opts map[string]string) g.Node

// overviewCardRenderers is keyed by store.OverviewCards ids.
var overviewCardRenderers = map[string]cardRenderer{
	"kpis":     kpisCard,
	"tir":      tirCard,
	"agp":      agpCard,
	"trend":    trendCard,
	"heatmap":  heatmapCard,
	"calendar": calendarCard,
	"dayparts": dayPartsCard,
	"episodes": episodesCard,
	"by_sport": bySportCard,
	"insights": insightsCard,
	"table":    statsTableCard,
	"sources":  sourcesCard,
}

// needsReadings marks the cards that are empty without glucose readings in
// the range; without any, the page shows one explanation instead of eight.
var needsReadings = map[string]bool{
	"kpis": true, "tir": true, "agp": true, "trend": true, "heatmap": true,
	"calendar": true, "dayparts": true, "episodes": true,
}

// halfWidth cards sit two to a row on wide screens; the rest span the row.
// Cards with wide tables (time of day, lows and highs) need the full row.
var halfWidth = map[string]bool{"heatmap": true, "calendar": true}

// mondayRow maps Go's weekday (Sunday = 0) to a Monday-first row.
func mondayRow(w time.Weekday) int { return (int(w) + 6) % 7 }

func ovCard(id, title, sub string, body ...g.Node) g.Node {
	return Card(append([]g.Node{ID("ov-" + id), H2(g.Text(title)),
		g.If(sub != "", P(Class("muted"), g.Text(sub)))}, body...)...)
}

// ---- key numbers

func kpisCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	m, k := d.Model, d.Model.Cur
	sub := d.Range.Label(tr, d.Loc)
	if m.Prev != nil {
		if m.Prev.HasData {
			sub = tr.T("overview.kpis.sub", "range", sub, "prev", d.Range.prevLabel(tr, d.Loc))
		} else {
			sub = tr.T("overview.kpis.sub_nodata", "range", sub, "prev", d.Range.prevLabel(tr, d.Loc))
		}
	}
	p := m.Prev
	delta := func(f func(periodKPIs) float64, better int, unit string, scale float64, dec int) *Delta {
		if p == nil || !p.HasData {
			return nil
		}
		return kpiDelta(tr, f(k), f(*p), better, unit, scale, dec)
	}
	avgUnit, avgScale, avgDec := "mg/dL", 1.0, 0
	if d.Unit == render.MmolL {
		avgUnit, avgScale, avgDec = "mmol/L", 1/18.016, 1
	}
	risk := k.GRI.Zone
	if key, ok := zoneKeys[k.GRI.Zone]; ok {
		risk = tr.T(key) // i18n:dynamic
	}
	longest := tr.T("kpi.gaps.none")
	if k.Coverage.Longest > 0 {
		longest = tr.T("kpi.gaps.longest", "duration", fmtDurationT(tr, k.Coverage.Longest))
	}
	return ovCard("kpis", tr.T("overview.kpis.title"), sub, Grid("",
		StatTile(tr.T("kpi.tir"), pctT(tr, k.TIR.InRange, 0), tr.T("kpi.tir.sub", "below", tr.Num(k.TIR.Below(), 0), "above", tr.Num(k.TIR.Above(), 0)),
			delta(func(x periodKPIs) float64 { return x.TIR.InRange }, 1, "pts", 1, 1)),
		StatTile(tr.T("kpi.avg"), valueT(tr, k.Avg, d.Unit), string(d.Unit),
			delta(func(x periodKPIs) float64 { return x.Avg }, 0, avgUnit, avgScale, avgDec)),
		StatTile(tr.T("kpi.gmi"), pctT(tr, k.GMI, 1), tr.T("kpi.gmi.sub"),
			delta(func(x periodKPIs) float64 { return x.GMI }, -1, "pts", 1, 1)),
		StatTile(tr.T("kpi.cv"), pctT(tr, k.CV, 0), tr.T("kpi.cv.sub", "n", tr.Num(cvTarget, 0)),
			delta(func(x periodKPIs) float64 { return x.CV }, -1, "pts", 1, 1)),
		StatTile(tr.T("kpi.gri"), tr.Num(k.GRI.Score, 0), tr.T("kpi.gri.sub", "zone", k.GRI.Zone, "risk", risk),
			delta(func(x periodKPIs) float64 { return x.GRI.Score }, -1, "pts", 1, 0)),
		StatTile(tr.T("kpi.coverage"), pctT(tr, k.Coverage.Pct, 0), tr.T("kpi.coverage.sub", "days", tr.Tn("fmt.days", k.Days), "longest", longest),
			delta(func(x periodKPIs) float64 { return x.Coverage.Pct }, 1, "pts", 1, 1)),
	), artifactNote(d))
}

// prevLabel describes the comparison window.
func (r statsRange) prevLabel(tr *i18n.Translator, loc *time.Location) string {
	return rangeTextT(tr, r.PrevFrom, r.PrevTo, loc)
}

// ---- time in range

type bandRow struct {
	label, bound, color string
	pct                 float64
}

func bandRows(tr *i18n.Translator, t analytics.TIR5, thr analytics.Thresholds, u render.Unit) []bandRow {
	v := func(x float64) string { return valueT(tr, x, u) }
	vlow, low, inr, high, vhigh := bandNames(tr)
	return []bandRow{
		{vlow, tr.T("band.bound.below", "v", v(thr.VeryLow)), ColVeryLow, t.VeryLow},
		{low, v(thr.VeryLow) + "–" + v(thr.Low), ColLow, t.Low},
		{inr, v(thr.Low) + "–" + v(thr.High), ColInRange, t.InRange},
		{high, v(thr.High) + "–" + v(thr.VeryHigh), ColHigh, t.High},
		{vhigh, tr.T("band.bound.above", "v", v(thr.VeryHigh)), ColVeryHigh, t.VeryHigh},
	}
}

func tirDonut(id, title string, t analytics.TIR5, d StatsData) g.Node {
	tr := d.tr()
	if t.Count == 0 {
		return Div(H3(g.Text(title)), EmptyState("inbox", tr.T("overview.tir.empty"), tr.T("overview.tir.empty_hint")))
	}
	rows := bandRows(tr, t, d.Thr, d.Unit)
	slices := make([]DonutSlice, 0, len(rows))
	items := make([]g.Node, 0, len(rows))
	for _, r := range rows {
		slices = append(slices, DonutSlice{Label: r.label, Value: r.pct, Color: r.color})
		items = append(items, Div(Class("flex items-center gap-2 py-0.5 text-sm"),
			Span(Class("size-2.5 shrink-0 rounded-full"), g.Attr("style", "background:"+r.color)),
			Span(Class("flex-1"), g.Text(r.label), Span(Class("ml-1.5 text-xs text-ink-2"), g.Text(r.bound+" "+string(d.Unit)))),
			Span(Class("font-semibold tabular-nums"), g.Text(pctT(tr, r.pct, 1))),
		))
	}
	return Div(H3(g.Text(title)),
		Chart(id, DonutOption(DonutInput{Slices: slices, Centre: pctT(tr, t.InRange, 0), Sub: tr.T("chart.in_range")}), 200),
		Div(Class("mt-1"), g.Group(items)),
		P(Class("muted mt-2 mb-0 text-xs"), g.Text(tr.Tn("fmt.readings", t.Count))),
	)
}

func tirCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	m := d.Model
	return ovCard("tir", tr.T("overview.tir.title"), tr.T("overview.tir.sub", "range", d.Range.Label(tr, d.Loc)),
		Grid("2",
			tirDonut("ov-tir-all", tr.T("overview.tir.all"), analytics.TIR5{
				VeryLow: m.Cur.TIR.VeryLow, Low: m.Cur.TIR.Low, InRange: m.Cur.TIR.InRange, High: m.Cur.TIR.High,
				VeryHigh: m.Cur.TIR.VeryHigh, Count: m.Cur.Count,
			}, d),
			tirDonut("ov-tir-act", tr.T("overview.tir.during"), m.TIRDuring, d),
		),
		P(Class("muted mb-0 text-xs"), g.Text(tr.T("overview.tir.consensus"))),
	)
}

// ---- AGP

func agpCard(d StatsData, opts map[string]string) g.Node {
	tr := d.tr()
	m := d.Model
	agp, sub := m.AGPAll, tr.T("overview.agp.sub")
	if opts["hours"] == "rest" {
		agp, sub = m.AGPRest, tr.T("overview.agp.sub_rest")
	}
	pts := make([]AGPPoint, 0, len(agp.Bins))
	for _, b := range agp.Bins {
		if b.N == 0 || b.Sparse {
			continue
		}
		pts = append(pts, AGPPoint{Minute: b.Minute, P5: b.P5, P25: b.P25, P50: b.P50, P75: b.P75, P95: b.P95})
	}
	if len(pts) < 8 {
		return ovCard("agp", tr.T("overview.agp.title"), sub, EmptyState("chart", tr.T("overview.agp.empty"),
			tr.T("overview.agp.empty_hint")))
	}
	return ovCard("agp", tr.T("overview.agp.title"), tr.T("overview.agp.sub_days", "sub", sub, "days", tr.Tn("fmt.days", agp.Days)),
		Chart("ov-agp", AGPOption(AGPInput{Points: pts, Low: d.Thr.Low, High: d.Thr.High, Unit: d.Unit, T: tr}), 320))
}

// ---- daily trends

func trendCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	days := d.Model.Daily
	if len(days) == 0 {
		return ovCard("trend", tr.T("overview.trend.title"), "", EmptyState("chart", tr.T("overview.trend.empty"), ""))
	}
	avg, mn, mx := make([]TrendPoint, len(days)), make([]TrendPoint, len(days)), make([]TrendPoint, len(days))
	tir, cv := make([]TrendPoint, len(days)), make([]TrendPoint, len(days))
	for i, day := range days {
		t := day.Date.UnixMilli()
		avg[i], mn[i], mx[i] = TrendPoint{t, day.Avg}, TrendPoint{t, day.Min}, TrendPoint{t, day.Max}
		tir[i], cv[i] = TrendPoint{t, day.TIR.InRange}, TrendPoint{t, day.CV}
	}
	span := trendSpan(tr, days[0].Date, days[len(days)-1].Date)
	return ovCard("trend", tr.T("overview.trend.title"), tr.T("overview.trend.sub", "span", span),
		Grid("2",
			Div(H3(g.Text(tr.T("overview.trend.glucose"))), Chart("ov-trend-glucose", TrendOption(TrendInput{
				Series: []TrendSeries{
					{Name: tr.T("chart.average"), Color: colLine, Points: avg},
					{Name: tr.T("chart.daily_max"), Color: ColHigh, Points: mx, Thin: true},
					{Name: tr.T("chart.daily_min"), Color: ColLow, Points: mn, Thin: true},
				},
				Low: d.Thr.Low, High: d.Thr.High, Unit: d.Unit, Zoom: true,
			}), 300)),
			Div(H3(g.Text(tr.T("overview.trend.tir_cv"))), Chart("ov-trend-tir", TrendOption(TrendInput{
				Series: []TrendSeries{
					{Name: tr.T("kpi.tir"), Color: ColInRange, Points: tir},
					{Name: tr.T("kpi.cv"), Color: colLine, Points: cv, Thin: true},
				},
				Plain: true, Format: fmtPct, Zoom: true,
			}), 300)),
		),
	)
}

// ---- heatmaps

// tirScaleLegend explains the colours of the time-in-range matrices; the
// gradient mirrors the visual map of PctMatrixOption.
func tirScaleLegend(tr *i18n.Translator) g.Node {
	return Div(append(comp("legend"),
		Span(g.Text(tr.T("overview.legend.tir_low"))),
		Span(Class("h-2.5 w-40 rounded-full"), g.Attr("style", "background:linear-gradient(90deg,"+ColLow+","+ColHigh+","+ColInRange+")")),
		Span(g.Text(tr.T("overview.legend.tir_high"))))...)
}

func rangeLegend(d StatsData) g.Node {
	tr := d.tr()
	item := func(color, label string) g.Node {
		return Span(Class("inline-flex items-center gap-1.5"),
			Span(Class("size-2.5 rounded-sm"), g.Attr("style", "background:"+color)), g.Text(label))
	}
	return Div(append(comp("legend"),
		item(ColLow, tr.T("overview.legend.below", "v", valueT(tr, d.Thr.Low, d.Unit))),
		item(ColInRange, tr.T("chart.in_range")),
		item(ColHigh, tr.T("overview.legend.above", "v", valueT(tr, d.Thr.High, d.Unit))))...)
}

func heatmapCard(d StatsData, opts map[string]string) g.Node {
	tr := d.tr()
	weekdays := weekdaysMondayFirst(tr)
	m := d.Model
	hours := make([]string, 24)
	for h := range hours {
		hours[h] = fmt.Sprintf("%02d", h)
	}
	if opts["metric"] != "mean" { // time in range is the default colouring
		var cells []PctCell
		for w := time.Sunday; w <= time.Saturday; w++ {
			for h := 0; h < 24; h++ {
				c := m.Heat[w][h]
				if c.Count == 0 {
					continue
				}
				cells = append(cells, PctCell{X: h, Y: mondayRow(w), Pct: c.InRange, Label: fmt.Sprintf("%s %02d:00", weekdays[mondayRow(w)], h)})
			}
		}
		return ovCard("heatmap", tr.T("overview.heatmap.title"), tr.T("overview.heatmap.sub_tir"),
			Chart("ov-heatmap", PctMatrixOption(PctMatrixInput{XLabels: hours, YLabels: weekdays, Cells: cells}), 280),
			tirScaleLegend(tr))
	}
	var cells []HeatCell
	for w := time.Sunday; w <= time.Saturday; w++ {
		for h := 0; h < 24; h++ {
			if c := m.Heat[w][h]; c.Count > 0 {
				cells = append(cells, HeatCell{Day: mondayRow(w), Hour: h, Avg: c.Mean})
			}
		}
	}
	return ovCard("heatmap", tr.T("overview.heatmap.title"), tr.T("overview.heatmap.sub_mean"),
		Chart("ov-heatmap", HeatmapOption(HeatmapInput{Days: weekdays, Cells: cells, Low: d.Thr.Low, High: d.Thr.High, Unit: d.Unit}), 280),
		rangeLegend(d))
}

// calendarWeeks caps the calendar at a year, so "all time" stays legible.
const calendarWeeks = 53

// mondayOf is local midnight of the Monday on or before t.
func mondayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day()-mondayRow(t.Weekday()), 0, 0, 0, 0, t.Location())
}

func calendarCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	weekdays := weekdaysMondayFirst(tr)
	days := d.Model.Daily
	if len(days) == 0 {
		return ovCard("calendar", tr.T("overview.calendar.title"), "", EmptyState("chart", tr.T("overview.trend.empty"), ""))
	}
	last := mondayOf(days[len(days)-1].Date)
	first := mondayOf(days[0].Date)
	if weeks := int(last.Sub(first).Round(24*time.Hour) / (7 * 24 * time.Hour)); weeks >= calendarWeeks {
		first = last.AddDate(0, 0, -7*(calendarWeeks-1))
	}
	// Weeks are counted by calendar arithmetic, not by dividing durations: a
	// week that holds a DST change is not 168 hours long.
	var xLabels []string
	weekIdx := map[time.Time]int{}
	for w := first; !w.After(last); w = w.AddDate(0, 0, 7) {
		weekIdx[w] = len(xLabels)
		xLabels = append(xLabels, tr.Date(w, false))
	}
	var cells []PctCell
	for _, day := range days {
		x, ok := weekIdx[mondayOf(day.Date)]
		if !ok {
			continue
		}
		cells = append(cells, PctCell{X: x, Y: mondayRow(day.Date.Weekday()), Pct: day.TIR.InRange, Label: weekdays[mondayRow(day.Date.Weekday())] + " " + tr.Date(day.Date, false)})
	}
	every := 1
	if len(xLabels) > 12 {
		every = 4
	}
	return ovCard("calendar", tr.T("overview.calendar.title"), tr.T("overview.calendar.sub"),
		Chart("ov-calendar", PctMatrixOption(PctMatrixInput{XLabels: xLabels, YLabels: weekdays, Cells: cells, XEvery: every}), 230),
		tirScaleLegend(tr))
}

// ---- time of day

func dayPartsCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	parts := d.Model.Parts
	cats := make([]string, 0, 4)
	vlow, low, inr, high, vhigh := bandNames(tr)
	series := []BarSeries{
		{Name: vlow, Color: ColVeryLow, Stack: "tir"}, {Name: low, Color: ColLow, Stack: "tir"},
		{Name: inr, Color: ColInRange, Stack: "tir"}, {Name: high, Color: ColHigh, Stack: "tir"},
		{Name: vhigh, Color: ColVeryHigh, Stack: "tir"},
	}
	rows := make([]g.Node, 0, 4)
	for _, p := range parts {
		name := partNameT(tr, p.Name)
		cats = append(cats, fmt.Sprintf("%s %02d–%02d", name, p.FromHour, p.ToHour))
		t := p.TIR
		for i, v := range []float64{t.VeryLow, t.Low, t.InRange, t.High, t.VeryHigh} {
			series[i].Values = append(series[i].Values, round2(v))
		}
		if p.Count == 0 {
			rows = append(rows, Tr(Td(g.Text(name)), Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("0"))))
			continue
		}
		rows = append(rows, Tr(Td(g.Text(name)),
			Td(Class("num"), g.Text(pctT(tr, t.InRange, 0))), Td(Class("num"), g.Text(valueT(tr, p.Avg, d.Unit))),
			Td(Class("num"), g.Text(pctT(tr, p.CV, 0))), Td(Class("num"), g.Text(tr.Int(p.Count)))))
	}
	return ovCard("dayparts", tr.T("overview.dayparts.title"), tr.T("overview.dayparts.sub"),
		Chart("ov-dayparts", BarOption(BarInput{Categories: cats, Series: series, Format: fmtPct, Max: 100, Legend: true}), 280),
		Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text(tr.T("overview.dayparts.part"))), Th(Class("num"), g.Text(inr)), Th(Class("num"), g.Text(tr.T("kpi.avg"))),
				Th(Class("num"), g.Text(tr.T("overview.dayparts.cv"))), Th(Class("num"), g.Text(tr.T("overview.dayparts.readings"))))),
			TBody(g.Group(rows)))...))...))
}

// ---- episodes

func episodesCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	m := d.Model
	vlow, _, _, _, vhigh := bandNames(tr)
	kinds := []struct {
		kind  analytics.EpisodeKind
		label string
	}{
		{analytics.KindVeryLow, vlow}, {analytics.KindLow, tr.T("overview.episodes.low_incl")},
		{analytics.KindHigh, tr.T("overview.episodes.high_incl")}, {analytics.KindVeryHigh, vhigh},
	}
	rows := make([]g.Node, 0, len(kinds))
	for _, k := range kinds {
		st := analytics.SummarizeEpisodes(analytics.OfKind(m.Episodes, k.kind))
		if st.Count == 0 {
			rows = append(rows, Tr(Td(g.Text(k.label)), Td(Class("num"), g.Text("0")),
				Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("-"))))
			continue
		}
		rows = append(rows, Tr(Td(g.Text(k.label)), Td(Class("num"), g.Text(tr.Int(st.Count))),
			Td(Class("num"), g.Text(tr.Int(st.Nocturnal))),
			Td(Class("num"), g.Textf("%s / %s", fmtDurationT(tr, st.Longest), fmtDurationT(tr, st.Total))),
			Td(Class("num"), g.Text(valueT(tr, st.Extreme, d.Unit)))))
	}
	summary := Div(append(comp("tablewrap"), Table(append(comp("table"),
		THead(Tr(Th(g.Text(tr.T("overview.episodes.kind"))), Th(Class("num"), g.Text(tr.T("overview.episodes.count"))), Th(Class("num"), g.Text(tr.T("overview.episodes.night_count"))),
			Th(Class("num"), g.Text(tr.T("overview.episodes.longest_total"))), Th(Class("num"), g.Text(tr.T("overview.episodes.extreme"))))),
		TBody(g.Group(rows)))...))...)

	recent := recentEpisodes(m.Episodes, 8)
	var list g.Node
	if len(recent) == 0 {
		list = P(Class("muted mt-3 mb-0"), g.Text(tr.T("overview.episodes.none")))
	} else {
		items := make([]g.Node, 0, len(recent))
		for _, e := range recent {
			items = append(items, episodeRow(e, m, d))
		}
		list = Div(H3(Class("mt-4"), g.Text(tr.T("overview.episodes.recent"))),
			Div(append(comp("tablewrap"), Table(append(comp("table"),
				THead(Tr(Th(g.Text(tr.T("overview.episodes.when"))), Th(g.Text(tr.T("overview.episodes.kind"))), Th(Class("num"), g.Text(tr.T("overview.episodes.duration"))),
					Th(Class("num"), g.Text(tr.T("overview.episodes.extreme"))), Th(g.Text(tr.T("overview.episodes.around"))))),
				TBody(g.Group(items)))...))...))
	}
	return ovCard("episodes", tr.T("overview.episodes.title"), tr.T("overview.episodes.sub"), summary, artifactNote(d), list, leftOutList(d))
}

func episodeRow(e analytics.Episode, m *overviewModel, d StatsData) g.Node {
	tr := d.tr()
	var badge g.Node
	switch {
	case e.Kind == analytics.KindLow && e.Extreme < d.Thr.VeryLow:
		badge = Badge("error", tr.T("band.very_low"))
	case e.Kind == analytics.KindLow:
		badge = Badge("warning", tr.T("band.low"))
	case e.Extreme > d.Thr.VeryHigh:
		badge = Badge("error", tr.T("band.very_high"))
	default:
		badge = Badge("warning", tr.T("band.high"))
	}
	around := g.Node(Span(Class("muted"), g.Text("-")))
	if a, ok := nearbyActivity(m.Acts, e); ok {
		name := a.Name
		if name == "" {
			name = tr.T("activity.untitled", "id", a.StravaID)
		}
		when := tr.T("overview.episodes.during") + " "
		if e.Start.After(a.End()) {
			when = tr.T("overview.episodes.after") + " "
		}
		around = g.Group([]g.Node{Span(Class("muted"), g.Text(when)), A(Href("/activity/"+a.StravaID), g.Text(name))})
	}
	art := artifactFor(m, e)
	var suspect g.Node = g.Group(nil)
	if art != nil {
		suspect = Span(Class("ml-1"), g.Attr("title", artifactDetailT(tr, *art, d.Unit)), Badge("info", artifactLabelT(tr, *art)))
	}
	return Tr(
		Td(g.Text(fmtWhenT(tr, e.Start, d.Loc, d.Now)), g.If(e.Nocturnal, Span(Class("muted"), g.Text(" · "+tr.T("overview.episodes.night"))))),
		Td(badge, suspect, Div(Class("mt-1 flex flex-wrap gap-1"), episodeActions(e, art, d))), Td(Class("num"), g.Text(fmtDurationT(tr, e.Duration))),
		Td(Class("num"), g.Text(valueT(tr, e.Extreme, d.Unit))), Td(around))
}

// ---- by activity type

func bySportCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	sports := d.Model.Sports
	if len(sports) == 0 {
		return ovCard("by_sport", tr.T("overview.bysport.title"), "", EmptyState("activity", tr.T("overview.bysport.empty"),
			tr.T("overview.bysport.empty_hint")))
	}
	cats := make([]string, len(sports))
	band := func(pick func(analytics.TIR5) float64) []float64 {
		v := make([]float64, len(sports))
		for i, s := range sports {
			v[i] = round2(pick(s.Bands))
		}
		return v
	}
	rows := make([]g.Node, len(sports))
	for i, s := range sports {
		cats[i] = fmt.Sprintf("%s (%d)", s.Sport, s.Count)
		pace := "–"
		if p := render.FormatPace(s.Sport, s.TotalDistance, s.Duration); p != "" {
			pace = p
		}
		hr := "–"
		if s.AvgHR > 0 {
			hr = tr.Num(s.AvgHR, 0) + " bpm"
		}
		dist := "–"
		if s.TotalDistance > 0 {
			dist = tr.Num(s.TotalDistance/1000, 1) + " km"
		}
		rows[i] = Tr(Td(g.Text(s.Sport)), Td(Class("num"), g.Text(tr.Int(s.Count))),
			Td(Class("num"), g.Text(pctT(tr, s.TIR, 0))), Td(Class("num"), g.Text(pctT(tr, s.CV, 0))),
			Td(Class("num"), g.Text(signedGlucoseT(tr, s.Delta, d.Unit))),
			Td(Class("num"), g.Text(tr.Num(s.DropRate*unitScale(d.Unit), 1))),
			Td(Class("num"), g.Text(pctT(tr, s.PostLowShare, 0))),
			Td(Class("num"), g.Text(hr)),
			Td(Class("num"), g.Text(dist)), Td(Class("num"), g.Text(tr.Num(s.TotalElevation, 0)+" m")),
			Td(Class("num"), g.Text(pace)))
	}
	vlow, low, inr, high, vhigh := bandNames(tr)
	return ovCard("by_sport", tr.T("overview.bysport.title"), tr.T("overview.bysport.sub"),
		Chart("ov-bysport", BarOption(BarInput{
			Categories: cats, Horizontal: true, Format: fmtPct, Max: 100, Legend: true,
			Series: []BarSeries{
				{Name: vlow, Color: ColVeryLow, Stack: "tir", Values: band(func(t analytics.TIR5) float64 { return t.VeryLow })},
				{Name: low, Color: ColLow, Stack: "tir", Values: band(func(t analytics.TIR5) float64 { return t.Low })},
				{Name: inr, Color: ColInRange, Stack: "tir", Values: band(func(t analytics.TIR5) float64 { return t.InRange })},
				{Name: high, Color: ColHigh, Stack: "tir", Values: band(func(t analytics.TIR5) float64 { return t.High })},
				{Name: vhigh, Color: ColVeryHigh, Stack: "tir", Values: band(func(t analytics.TIR5) float64 { return t.VeryHigh })},
			},
		}), 90+40*len(sports)),
		Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text(tr.T("overview.bysport.type"))), Th(Class("num"), g.Text(tr.T("overview.bysport.activities"))), Th(Class("num"), g.Text(tr.T("kpi.tir"))),
				Th(Class("num"), g.Text(tr.T("overview.dayparts.cv"))), Th(Class("num"), g.Text(tr.T("overview.bysport.start_end", "unit", string(d.Unit)))),
				Th(Class("num"), g.Text(tr.T("overview.bysport.drop", "unit", string(d.Unit)))), Th(Class("num"), g.Text(tr.T("overview.bysport.low_after"))),
				Th(Class("num"), g.Text(tr.T("overview.bysport.avg_hr"))), Th(Class("num"), g.Text(tr.T("overview.bysport.distance"))), Th(Class("num"), g.Text(tr.T("overview.bysport.climb"))), Th(Class("num"), g.Text(tr.T("overview.bysport.pace"))))),
			TBody(g.Group(rows)))...))...),
		P(Class("muted text-sm"), g.Text(tr.T("overview.bysport.note"))),
	)
}

// unitScale converts a mg/dL difference into unit.
func unitScale(u render.Unit) float64 {
	if u == render.MmolL {
		return 1 / stats.MmolFactor
	}
	return 1
}

// insightsCard shows how glucose behaves around activities: start glucose
// against the change during the activity, the best and worst activities by
// time in range, and lows in the hours after.
func insightsCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	ins := d.Model.Insights
	withData := 0
	for _, i := range ins {
		if i.HasData {
			withData++
		}
	}
	if withData < 2 {
		return ovCard("insights", tr.T("overview.insights.title"), "", EmptyState("chart", tr.T("overview.insights.empty"),
			tr.T("overview.insights.empty_hint")))
	}
	sc := analytics.Scatter(ins)
	pts := make([]ScatterPoint, len(sc))
	for i, p := range sc {
		pts[i] = ScatterPoint{X: p.X, Y: p.Y, Label: p.Sport + " · " + dayMonthT(tr, p.Start, d.Loc)}
	}
	lows, postLows := 0, 0
	for _, i := range ins {
		if i.HasData {
			lows++
			if i.PostLow() {
				postLows++
			}
		}
	}
	n := withData / 2 // best and worst never overlap, so a short list needs a short pick
	if n > 3 {
		n = 3
	}
	best, worst := analytics.BestWorst(ins, n)
	list := func(title string, xs []analytics.ActivityInsight) g.Node {
		items := make([]g.Node, len(xs))
		for i, x := range xs {
			items[i] = Li(Class("flex justify-between gap-3 py-1"),
				A(Href("/activity/"+x.ID), g.Textf("%s · %s", x.Sport, dayMonthT(tr, x.Start, d.Loc))),
				Span(Class("num"), g.Textf("%s · %s→%s %s", pctT(tr, x.TIR.InRange, 0),
					valueT(tr, x.StartGlucose, d.Unit), valueT(tr, x.EndGlucose, d.Unit), d.Unit)))
		}
		return Div(H3(g.Text(title)), Ul(g.Group(items)))
	}
	return ovCard("insights", tr.T("overview.insights.title"), tr.Tn("overview.insights.sub", withData),
		Grid("",
			StatTile(tr.T("overview.insights.low_after"), tr.T("overview.insights.low_after_value", "a", postLows, "b", lows), tr.T("overview.insights.low_after_sub"), nil),
		),
		Chart("ov-insight-scatter", ScatterOption(ScatterInput{
			Points: pts, XName: tr.T("overview.insights.x"), YName: tr.T("overview.insights.y"),
			XFormat: glucoseFmt(d.Unit), YFormat: glucoseFmt(d.Unit), Color: colLine,
		}), 300),
		P(Class("muted text-sm"), g.Text(tr.T("overview.insights.note"))),
		Grid("2", list(tr.T("overview.insights.best"), best), list(tr.T("overview.insights.worst"), worst)),
	)
}

// ---- table

func statsTableCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	acts := d.Model.Acts
	return ovCard("table", tr.T("overview.table.title"), tr.T("overview.table.sub", "n", len(acts)),
		activityTable(DashData{Acts: acts, Unit: d.Unit, Loc: d.Loc, Now: d.Now, T: tr}))
}

// ---- sources and coverage

const chipClass = "flex items-center gap-2 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm"

func sourcesCard(d StatsData, _ map[string]string) g.Node {
	tr := d.tr()
	if len(d.Sources) == 0 {
		return ovCard("sources", tr.T("overview.sources.title"), "", EmptyState("inbox", tr.T("overview.sources.empty"),
			tr.T("overview.sources.empty_hint")))
	}
	chips := make([]g.Node, 0, len(d.Sources))
	for _, si := range d.Sources {
		chips = append(chips, Div(Class(chipClass), Code(g.Text(si.Source)),
			Span(Class("text-ink-2"), g.Text(tr.Tn("fmt.readings", int(si.Count)))),
			Span(Class("text-ink-2"), g.Text(tr.T("overview.sources.newest", "when", fmtWhenT(tr, si.Latest, d.Loc, d.Now))))))
	}
	sub := tr.T("overview.sources.sub")
	body := []g.Node{Div(Class("flex flex-wrap gap-2"), g.Group(chips))}
	if c := d.Model.Cur.Coverage; d.Model.HasData {
		line := tr.T("overview.sources.coverage", "pct", tr.Num(c.Pct, 0))
		if len(c.Gaps) > 0 {
			gaps := append([]analytics.Gap(nil), c.Gaps...)
			sort.Slice(gaps, func(i, j int) bool { return gaps[i].Duration > gaps[j].Duration })
			l := gaps[0]
			line += " " + tr.Tn("overview.sources.gaps", len(gaps), "duration", fmtDurationT(tr, l.Duration), "when", fmtWhenT(tr, l.Start, d.Loc, d.Now))
		} else {
			line += " " + tr.T("overview.sources.no_gaps")
		}
		body = append(body, P(Class("muted mt-3 mb-0"), g.Text(line)))
	}
	return ovCard("sources", tr.T("overview.sources.title"), sub, body...)
}

// ---- toolbar

// rangeToolbar is the preset switch, the custom-range form and the compare
// switch. All of it is plain links and a GET form, so it works without
// JavaScript and the address bar always holds the current view.
func rangeToolbar(d StatsData) g.Node {
	tr := d.tr()
	r := d.Range
	items := make([]NavItem, 0, len(store.OverviewRanges))
	for _, k := range store.OverviewRanges {
		items = append(items, NavItem{Key: k, Label: rangeLabelT(tr, k), Href: r.presetHref(k)})
	}
	custom := g.El("details", Class("relative"), g.If(r.Key == "custom", g.Attr("open", "")),
		g.El("summary", Class(customSummaryClass), g.Attr("data-active", boolAttr(r.Key == "custom")), g.Text(tr.T("overview.toolbar.custom"))),
		Form(Method("get"), Action("/stats"), Class(customFormClass),
			Div(Label(g.Text(tr.T("overview.toolbar.from")), Input(Type("date"), Name("from"), Required(), g.If(r.Key == "custom", Value(r.FromStr)))),
				Label(g.Text(tr.T("overview.toolbar.to")), Input(Type("date"), Name("to"), Required(), g.If(r.Key == "custom", Value(r.ToStr))))),
			g.If(r.Compare, Input(Type("hidden"), Name("compare"), Value("prev"))),
			SubmitBtn("primary", "sm", tr.T("overview.toolbar.show")),
		),
	)
	cmp := []NavItem{{Key: "off", Label: tr.T("overview.toolbar.cmp_off"), Href: r.href(false)}, {Key: "prev", Label: tr.T("overview.toolbar.cmp_prev"), Href: r.href(true)}}
	active := "off"
	if r.Compare {
		active = "prev"
	}
	return Div(Class("mb-4 flex flex-wrap items-center gap-2"),
		Segmented(tr.T("overview.toolbar.range"), items, presetActive(r)), custom,
		g.If(r.Key != "all", Span(Class("ml-auto"), Segmented(tr.T("overview.toolbar.comparison"), cmp, active))),
		A(append(comp("button"), Href(r.reportHref()), g.Attr("download", ""),
			g.Attr("data-variant", "ghost"), g.Attr("title", tr.T("overview.toolbar.report_hint")),
			icon("scroll", "size-4"), g.Text(tr.T("overview.toolbar.report")))...))
}

func presetActive(r statsRange) string {
	if r.Key == "custom" {
		return ""
	}
	return r.Key
}

func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

const (
	customSummaryClass = "cursor-pointer list-none rounded-lg border border-line bg-surface px-3 py-1.5 text-sm font-medium text-ink-2 hover:text-ink data-[active=true]:border-accent data-[active=true]:text-ink"
	customFormClass    = "absolute left-0 top-full z-30 mt-2 flex w-72 flex-col gap-3 rounded-xl border border-line bg-surface p-3 shadow-xl max-md:w-[min(18rem,calc(100vw-2rem))]"
)

// trendSpan formats the date range a per-day trend chart covers, so a chart
// is never shown without saying what time frame it's over.
func trendSpan(tr *i18n.Translator, first, last time.Time) string {
	return tr.T("fmt.range", "from", tr.Date(first, false), "to", tr.Date(last, false))
}

// StatsPage is the Overview: the configured cards over a selectable range.
func StatsPage(pd PageData, d StatsData) g.Node {
	tr := pd.translator()
	d.T = tr
	var cards []g.Node
	enabled := 0
	for _, c := range d.Cards {
		if !c.Enabled {
			continue
		}
		enabled++
		if !d.Model.HasData && needsReadings[c.ID] {
			continue
		}
		if draw, ok := overviewCardRenderers[c.ID]; ok {
			wrap := "min-w-0 xl:col-span-2"
			if halfWidth[c.ID] {
				wrap = "min-w-0 [&>*]:h-full"
			}
			cards = append(cards, Div(Class(wrap), draw(d, c.Options)))
		}
	}
	var lead g.Node
	switch {
	case enabled == 0:
		lead = Card(P(Class("muted"), g.Text(tr.T("overview.all_off"))))
	case !d.Model.HasData:
		lead = Card(EmptyState("inbox", tr.T("overview.empty.title"),
			tr.T("overview.empty.hint")))
	}
	return Page(pd,
		PageHead(tr.T("overview.title"), tr.T("overview.sub")),
		g.If(d.Range.Note != "", Notice("warning", g.Text(rangeNoteT(tr, d.Range.Note)))),
		rangeToolbar(d),
		Div(Class("grid grid-cols-1 gap-4 xl:grid-cols-2 xl:grid-flow-row-dense"),
			g.If(lead != nil, Div(Class("xl:col-span-2"), lead)), g.Group(cards)),
	)
}
