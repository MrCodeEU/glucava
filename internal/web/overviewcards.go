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

var weekdayLabels = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// mondayRow maps Go's weekday (Sunday = 0) to a Monday-first row.
func mondayRow(w time.Weekday) int { return (int(w) + 6) % 7 }

func ovCard(id, title, sub string, body ...g.Node) g.Node {
	return Card(append([]g.Node{ID("ov-" + id), H2(g.Text(title)),
		g.If(sub != "", P(Class("muted"), g.Text(sub)))}, body...)...)
}

func pct0(v float64) string { return fmt.Sprintf("%.0f%%", v) }

// ---- key numbers

func kpisCard(d StatsData, _ map[string]string) g.Node {
	m, k := d.Model, d.Model.Cur
	sub := d.Range.Label(d.Loc)
	if m.Prev != nil {
		sub += " · changes compare with " + d.Range.prevLabel(d.Loc)
		if !m.Prev.HasData {
			sub += " (no readings then)"
		}
	}
	p := m.Prev
	delta := func(f func(periodKPIs) float64, better int, unit string, scale float64, dec int) *Delta {
		if p == nil || !p.HasData {
			return nil
		}
		return kpiDelta(f(k), f(*p), better, unit, scale, dec)
	}
	avgUnit, avgScale, avgDec := "mg/dL", 1.0, 0
	if d.Unit == render.MmolL {
		avgUnit, avgScale, avgDec = "mmol/L", 1/18.016, 1
	}
	zone := map[string]string{"A": "lowest risk", "B": "low risk", "C": "moderate risk", "D": "high risk", "E": "highest risk"}
	longest := "no gaps"
	if k.Coverage.Longest > 0 {
		longest = "longest gap " + fmtDuration(k.Coverage.Longest)
	}
	return ovCard("kpis", "Key numbers", sub, Grid("",
		StatTile("Time in range", pct0(k.TIR.InRange), fmt.Sprintf("%.0f%% below · %.0f%% above", k.TIR.Below(), k.TIR.Above()),
			delta(func(x periodKPIs) float64 { return x.TIR.InRange }, 1, "pts", 1, 1)),
		StatTile("Average", render.Value(k.Avg, d.Unit), string(d.Unit),
			delta(func(x periodKPIs) float64 { return x.Avg }, 0, avgUnit, avgScale, avgDec)),
		StatTile("GMI", fmt.Sprintf("%.1f%%", k.GMI), "estimated A1C",
			delta(func(x periodKPIs) float64 { return x.GMI }, -1, "pts", 1, 1)),
		StatTile("Variability (CV)", pct0(k.CV), fmt.Sprintf("target ≤ %.0f%%", cvTarget),
			delta(func(x periodKPIs) float64 { return x.CV }, -1, "pts", 1, 1)),
		StatTile("Risk index (GRI)", fmt.Sprintf("%.0f", k.GRI.Score), "zone "+k.GRI.Zone+" · "+zone[k.GRI.Zone],
			delta(func(x periodKPIs) float64 { return x.GRI.Score }, -1, "pts", 1, 0)),
		StatTile("Data coverage", pct0(k.Coverage.Pct), fmt.Sprintf("%d days · %s", k.Days, longest),
			delta(func(x periodKPIs) float64 { return x.Coverage.Pct }, 1, "pts", 1, 1)),
	))
}

// prevLabel describes the comparison window.
func (r statsRange) prevLabel(loc *time.Location) string {
	return r.PrevFrom.In(loc).Format("2 Jan") + " – " + r.PrevTo.In(loc).Format("2 Jan")
}

// ---- time in range

type bandRow struct {
	label, bound, color string
	pct                 float64
}

func bandRows(t analytics.TIR5, thr analytics.Thresholds, u render.Unit) []bandRow {
	v := func(x float64) string { return render.Value(x, u) }
	return []bandRow{
		{"Very low", "below " + v(thr.VeryLow), ColVeryLow, t.VeryLow},
		{"Low", v(thr.VeryLow) + "–" + v(thr.Low), ColLow, t.Low},
		{"In range", v(thr.Low) + "–" + v(thr.High), ColInRange, t.InRange},
		{"High", v(thr.High) + "–" + v(thr.VeryHigh), ColHigh, t.High},
		{"Very high", "above " + v(thr.VeryHigh), ColVeryHigh, t.VeryHigh},
	}
}

func tirDonut(id, title string, t analytics.TIR5, d StatsData) g.Node {
	if t.Count == 0 {
		return Div(H3(g.Text(title)), EmptyState("inbox", "No readings", "Nothing was recorded in this part of the range."))
	}
	rows := bandRows(t, d.Thr, d.Unit)
	slices := make([]DonutSlice, 0, len(rows))
	items := make([]g.Node, 0, len(rows))
	for _, r := range rows {
		slices = append(slices, DonutSlice{Label: r.label, Value: r.pct, Color: r.color})
		items = append(items, Div(Class("flex items-center gap-2 py-0.5 text-sm"),
			Span(Class("size-2.5 shrink-0 rounded-full"), g.Attr("style", "background:"+r.color)),
			Span(Class("flex-1"), g.Text(r.label), Span(Class("ml-1.5 text-xs text-ink-2"), g.Text(r.bound+" "+string(d.Unit)))),
			Span(Class("font-semibold tabular-nums"), g.Textf("%.1f%%", r.pct)),
		))
	}
	return Div(H3(g.Text(title)),
		Chart(id, DonutOption(DonutInput{Slices: slices, Centre: pct0(t.InRange), Sub: "in range"}), 200),
		Div(Class("mt-1"), g.Group(items)),
		P(Class("muted mt-2 mb-0 text-xs"), g.Textf("%d readings", t.Count)),
	)
}

func tirCard(d StatsData, _ map[string]string) g.Node {
	m := d.Model
	return ovCard("tir", "Time in range", d.Range.Label(d.Loc)+" · five bands, every reading counts the same",
		Grid("2",
			tirDonut("ov-tir-all", "All readings, day and night", analytics.TIR5{
				VeryLow: m.Cur.TIR.VeryLow, Low: m.Cur.TIR.Low, InRange: m.Cur.TIR.InRange, High: m.Cur.TIR.High,
				VeryHigh: m.Cur.TIR.VeryHigh, Count: m.Cur.Count,
			}, d),
			tirDonut("ov-tir-act", "During activities", m.TIRDuring, d),
		),
		P(Class("muted mb-0 text-xs"), g.Text("Consensus targets: in range above 70%, below range under 4%, very low under 1%.")),
	)
}

// ---- AGP

func agpCard(d StatsData, opts map[string]string) g.Node {
	m := d.Model
	agp, sub := m.AGPAll, "Median with the 25–75% and 5–95% bands, by time of day"
	if opts["hours"] == "rest" {
		agp, sub = m.AGPRest, sub+", outside activities"
	}
	pts := make([]AGPPoint, 0, len(agp.Bins))
	for _, b := range agp.Bins {
		if b.N == 0 || b.Sparse {
			continue
		}
		pts = append(pts, AGPPoint{Minute: b.Minute, P5: b.P5, P25: b.P25, P50: b.P50, P75: b.P75, P95: b.P95})
	}
	if len(pts) < 8 {
		return ovCard("agp", "Daily profile (AGP)", sub, EmptyState("chart", "Not enough readings yet",
			"The profile needs a few days of readings to draw."))
	}
	return ovCard("agp", "Daily profile (AGP)", fmt.Sprintf("%s · %d days", sub, agp.Days),
		Chart("ov-agp", AGPOption(AGPInput{Points: pts, Low: d.Thr.Low, High: d.Thr.High, Unit: d.Unit}), 320))
}

// ---- daily trends

func trendCard(d StatsData, _ map[string]string) g.Node {
	days := d.Model.Daily
	if len(days) == 0 {
		return ovCard("trend", "Daily trends", "", EmptyState("chart", "No days with readings", ""))
	}
	avg, mn, mx := make([]TrendPoint, len(days)), make([]TrendPoint, len(days)), make([]TrendPoint, len(days))
	tir, cv := make([]TrendPoint, len(days)), make([]TrendPoint, len(days))
	for i, day := range days {
		t := day.Date.UnixMilli()
		avg[i], mn[i], mx[i] = TrendPoint{t, day.Avg}, TrendPoint{t, day.Min}, TrendPoint{t, day.Max}
		tir[i], cv[i] = TrendPoint{t, day.TIR.InRange}, TrendPoint{t, day.CV}
	}
	span := trendSpan(days[0].Date, days[len(days)-1].Date)
	return ovCard("trend", "Daily trends", span+" · one point per day, drag the slider to zoom",
		Grid("2",
			Div(H3(g.Text("Glucose")), Chart("ov-trend-glucose", TrendOption(TrendInput{
				Series: []TrendSeries{
					{Name: "Average", Color: colLine, Points: avg},
					{Name: "Daily max", Color: ColHigh, Points: mx, Thin: true},
					{Name: "Daily min", Color: ColLow, Points: mn, Thin: true},
				},
				Low: d.Thr.Low, High: d.Thr.High, Unit: d.Unit, Zoom: true,
			}), 300)),
			Div(H3(g.Text("Time in range and variability")), Chart("ov-trend-tir", TrendOption(TrendInput{
				Series: []TrendSeries{
					{Name: "Time in range", Color: ColInRange, Points: tir},
					{Name: "Variability (CV)", Color: colLine, Points: cv, Thin: true},
				},
				Plain: true, Format: fmtPct, Zoom: true,
			}), 300)),
		),
	)
}

// ---- heatmaps

// tirScaleLegend explains the colours of the time-in-range matrices; the
// bounds mirror tirPieces.
func tirScaleLegend() g.Node {
	item := func(color, label string) g.Node {
		return Span(Class("inline-flex items-center gap-1.5"),
			Span(Class("size-2.5 rounded-sm"), g.Attr("style", "background:"+color)), g.Text(label))
	}
	return Div(append(comp("legend"),
		item(ColLow, "under 50%"), item(ColHigh, "50–70%"), item("#8fd1b3", "70–90%"), item(ColInRange, "90% or more"))...)
}

func rangeLegend(d StatsData) g.Node {
	item := func(color, label string) g.Node {
		return Span(Class("inline-flex items-center gap-1.5"),
			Span(Class("size-2.5 rounded-sm"), g.Attr("style", "background:"+color)), g.Text(label))
	}
	return Div(append(comp("legend"),
		item(ColLow, "average below "+render.Value(d.Thr.Low, d.Unit)),
		item(ColInRange, "in range"),
		item(ColHigh, "above "+render.Value(d.Thr.High, d.Unit)))...)
}

func heatmapCard(d StatsData, opts map[string]string) g.Node {
	m := d.Model
	hours := make([]string, 24)
	for h := range hours {
		hours[h] = fmt.Sprintf("%02d", h)
	}
	if opts["metric"] == "tir" {
		var cells []PctCell
		for w := time.Sunday; w <= time.Saturday; w++ {
			for h := 0; h < 24; h++ {
				c := m.Heat[w][h]
				if c.Count == 0 {
					continue
				}
				cells = append(cells, PctCell{X: h, Y: mondayRow(w), Pct: c.InRange, Label: fmt.Sprintf("%s %02d:00", weekdayLabels[mondayRow(w)], h)})
			}
		}
		return ovCard("heatmap", "Weekday and hour", "Time in range for each hour of the week, by local time",
			Chart("ov-heatmap", PctMatrixOption(PctMatrixInput{XLabels: hours, YLabels: weekdayLabels, Cells: cells}), 280),
			tirScaleLegend())
	}
	var cells []HeatCell
	for w := time.Sunday; w <= time.Saturday; w++ {
		for h := 0; h < 24; h++ {
			if c := m.Heat[w][h]; c.Count > 0 {
				cells = append(cells, HeatCell{Day: mondayRow(w), Hour: h, Avg: c.Mean})
			}
		}
	}
	return ovCard("heatmap", "Weekday and hour", "Average glucose for each hour of the week, by local time",
		Chart("ov-heatmap", HeatmapOption(HeatmapInput{Days: weekdayLabels, Cells: cells, Low: d.Thr.Low, High: d.Thr.High, Unit: d.Unit}), 280),
		rangeLegend(d))
}

// calendarWeeks caps the calendar at a year, so "all time" stays legible.
const calendarWeeks = 53

// mondayOf is local midnight of the Monday on or before t.
func mondayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day()-mondayRow(t.Weekday()), 0, 0, 0, 0, t.Location())
}

func calendarCard(d StatsData, _ map[string]string) g.Node {
	days := d.Model.Daily
	if len(days) == 0 {
		return ovCard("calendar", "Calendar", "", EmptyState("chart", "No days with readings", ""))
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
		xLabels = append(xLabels, w.Format("2 Jan"))
	}
	var cells []PctCell
	for _, day := range days {
		x, ok := weekIdx[mondayOf(day.Date)]
		if !ok {
			continue
		}
		cells = append(cells, PctCell{X: x, Y: mondayRow(day.Date.Weekday()), Pct: day.TIR.InRange, Label: day.Date.Format("Mon 2 Jan")})
	}
	every := 1
	if len(xLabels) > 12 {
		every = 4
	}
	return ovCard("calendar", "Calendar", "Time in range for every day; each column is a week starting on Monday",
		Chart("ov-calendar", PctMatrixOption(PctMatrixInput{XLabels: xLabels, YLabels: weekdayLabels, Cells: cells, XEvery: every}), 230),
		tirScaleLegend())
}

// ---- time of day

func dayPartsCard(d StatsData, _ map[string]string) g.Node {
	parts := d.Model.Parts
	cats := make([]string, 0, 4)
	series := []BarSeries{
		{Name: "Very low", Color: ColVeryLow, Stack: "tir"}, {Name: "Low", Color: ColLow, Stack: "tir"},
		{Name: "In range", Color: ColInRange, Stack: "tir"}, {Name: "High", Color: ColHigh, Stack: "tir"},
		{Name: "Very high", Color: ColVeryHigh, Stack: "tir"},
	}
	rows := make([]g.Node, 0, 4)
	for _, p := range parts {
		cats = append(cats, fmt.Sprintf("%s %02d–%02d", p.Name, p.FromHour, p.ToHour))
		t := p.TIR
		for i, v := range []float64{t.VeryLow, t.Low, t.InRange, t.High, t.VeryHigh} {
			series[i].Values = append(series[i].Values, round2(v))
		}
		if p.Count == 0 {
			rows = append(rows, Tr(Td(g.Text(p.Name)), Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("0"))))
			continue
		}
		rows = append(rows, Tr(Td(g.Text(p.Name)),
			Td(Class("num"), g.Text(pct0(t.InRange))), Td(Class("num"), g.Text(render.Value(p.Avg, d.Unit))),
			Td(Class("num"), g.Text(pct0(p.CV))), Td(Class("num"), g.Textf("%d", p.Count))))
	}
	return ovCard("dayparts", "Time of day", "Share of readings in each band, by local time of day",
		Chart("ov-dayparts", BarOption(BarInput{Categories: cats, Series: series, Format: fmtPct, Max: 100, Legend: true}), 280),
		Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text("Part of day")), Th(Class("num"), g.Text("In range")), Th(Class("num"), g.Text("Average")),
				Th(Class("num"), g.Text("CV")), Th(Class("num"), g.Text("Readings")))),
			TBody(g.Group(rows)))...))...))
}

// ---- episodes

func episodesCard(d StatsData, _ map[string]string) g.Node {
	m := d.Model
	kinds := []struct {
		kind  analytics.EpisodeKind
		label string
	}{
		{analytics.KindVeryLow, "Very low"}, {analytics.KindLow, "Low (incl. very low)"},
		{analytics.KindHigh, "High (incl. very high)"}, {analytics.KindVeryHigh, "Very high"},
	}
	rows := make([]g.Node, 0, len(kinds))
	for _, k := range kinds {
		st := analytics.SummarizeEpisodes(analytics.OfKind(m.Episodes, k.kind))
		if st.Count == 0 {
			rows = append(rows, Tr(Td(g.Text(k.label)), Td(Class("num"), g.Text("0")),
				Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("-")), Td(Class("num"), g.Text("-"))))
			continue
		}
		rows = append(rows, Tr(Td(g.Text(k.label)), Td(Class("num"), g.Textf("%d", st.Count)),
			Td(Class("num"), g.Textf("%d", st.Nocturnal)), Td(Class("num"), g.Text(fmtDuration(st.Longest))),
			Td(Class("num"), g.Text(fmtDuration(st.Total))), Td(Class("num"), g.Text(render.Value(st.Extreme, d.Unit)))))
	}
	summary := Div(append(comp("tablewrap"), Table(append(comp("table"),
		THead(Tr(Th(g.Text("Kind")), Th(Class("num"), g.Text("Episodes")), Th(Class("num"), g.Text("At night")),
			Th(Class("num"), g.Text("Longest")), Th(Class("num"), g.Text("Total time")), Th(Class("num"), g.Text("Nadir / peak")))),
		TBody(g.Group(rows)))...))...)

	recent := recentEpisodes(m.Episodes, 8)
	var list g.Node
	if len(recent) == 0 {
		list = P(Class("muted mt-3 mb-0"), g.Text("No lows or highs of 15 minutes or more in this range."))
	} else {
		items := make([]g.Node, 0, len(recent))
		for _, e := range recent {
			items = append(items, episodeRow(e, m, d))
		}
		list = Div(H3(Class("mt-4"), g.Text("Most recent")),
			Div(append(comp("tablewrap"), Table(append(comp("table"),
				THead(Tr(Th(g.Text("When")), Th(g.Text("Kind")), Th(Class("num"), g.Text("Duration")),
					Th(Class("num"), g.Text("Nadir / peak")), Th(g.Text("Around")))),
				TBody(g.Group(items)))...))...))
	}
	return ovCard("episodes", "Lows and highs", "Runs of at least 15 minutes beyond the target range", summary, list)
}

func episodeRow(e analytics.Episode, m *overviewModel, d StatsData) g.Node {
	var badge g.Node
	switch {
	case e.Kind == analytics.KindLow && e.Extreme < d.Thr.VeryLow:
		badge = Badge("error", "Very low")
	case e.Kind == analytics.KindLow:
		badge = Badge("warning", "Low")
	case e.Extreme > d.Thr.VeryHigh:
		badge = Badge("error", "Very high")
	default:
		badge = Badge("warning", "High")
	}
	around := g.Node(Span(Class("muted"), g.Text("-")))
	if a, ok := nearbyActivity(m.Acts, e); ok {
		name := a.Name
		if name == "" {
			name = "Activity " + a.StravaID
		}
		when := "during "
		if e.Start.After(a.End()) {
			when = "after "
		}
		around = g.Group([]g.Node{Span(Class("muted"), g.Text(when)), A(Href("/activity/"+a.StravaID), g.Text(name))})
	}
	return Tr(
		Td(g.Text(fmtWhen(e.Start, d.Loc, d.Now)), g.If(e.Nocturnal, Span(Class("muted"), g.Text(" · night")))),
		Td(badge), Td(Class("num"), g.Text(fmtDuration(e.Duration))),
		Td(Class("num"), g.Text(render.Value(e.Extreme, d.Unit))), Td(around))
}

// ---- by activity type

func bySportCard(d StatsData, _ map[string]string) g.Node {
	sports := d.Model.Sports
	if len(sports) == 0 {
		return ovCard("by_sport", "By activity type", "", EmptyState("activity", "No completed activities in this range",
			"Activities show up here once glucava has processed them."))
	}
	cats, vals := make([]string, len(sports)), make([]float64, len(sports))
	rows := make([]g.Node, len(sports))
	for i, s := range sports {
		cats[i], vals[i] = fmt.Sprintf("%s (%d)", s.Sport, s.Count), round2(s.TIR)
		pace := "–"
		if p := render.FormatPace(s.Sport, s.TotalDistance, s.Duration); p != "" {
			pace = p
		}
		hr := "–"
		if s.AvgHR > 0 {
			hr = fmt.Sprintf("%.0f bpm", s.AvgHR)
		}
		dist := "–"
		if s.TotalDistance > 0 {
			dist = fmt.Sprintf("%.1f km", s.TotalDistance/1000)
		}
		rows[i] = Tr(Td(g.Text(s.Sport)), Td(Class("num"), g.Textf("%d", s.Count)),
			Td(Class("num"), g.Text(pct0(s.TIR))), Td(Class("num"), g.Text(pct0(s.CV))),
			Td(Class("num"), g.Text(signedGlucose(s.Delta, d.Unit))),
			Td(Class("num"), g.Text(fmt.Sprintf("%.1f", s.DropRate*unitScale(d.Unit)))),
			Td(Class("num"), g.Text(pct0(s.PostLowShare))),
			Td(Class("num"), g.Text(hr)),
			Td(Class("num"), g.Text(dist)), Td(Class("num"), g.Textf("%.0f m", s.TotalElevation)),
			Td(Class("num"), g.Text(pace)))
	}
	return ovCard("by_sport", "By activity type", "Averages per activity of each type, over activities with glucose data",
		Chart("ov-bysport", BarOption(BarInput{
			Categories: cats, Horizontal: true, Format: fmtPct, Max: 100,
			Series: []BarSeries{{Name: "Average time in range", Color: ColInRange, Values: vals}},
		}), 60+38*len(sports)),
		Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text("Type")), Th(Class("num"), g.Text("Activities")), Th(Class("num"), g.Text("Time in range")),
				Th(Class("num"), g.Text("CV")), Th(Class("num"), g.Textf("Start→end (%s)", d.Unit)),
				Th(Class("num"), g.Textf("Drop (%s/10 min)", d.Unit)), Th(Class("num"), g.Text("Low after")),
				Th(Class("num"), g.Text("Avg HR")), Th(Class("num"), g.Text("Distance")), Th(Class("num"), g.Text("Climb")), Th(Class("num"), g.Text("Pace")))),
			TBody(g.Group(rows)))...))...),
		P(Class("muted text-sm"), g.Text("“Low after” is the share of activities followed by a low within three hours. Drop is positive when glucose falls during the activity.")),
	)
}

// unitScale converts a mg/dL difference into unit.
func unitScale(u render.Unit) float64 {
	if u == render.MmolL {
		return 1 / stats.MmolFactor
	}
	return 1
}

// signedGlucose formats a mg/dL difference with an explicit sign.
func signedGlucose(v float64, u render.Unit) string {
	x := v * unitScale(u)
	dec := 0
	if u == render.MmolL {
		dec = 1
	}
	if x >= 0 {
		return fmt.Sprintf("+%.*f", dec, x)
	}
	return fmt.Sprintf("−%.*f", dec, -x)
}

// insightsCard shows how glucose behaves around activities: start glucose
// against the change during the activity, the best and worst activities by
// time in range, and lows in the hours after.
func insightsCard(d StatsData, _ map[string]string) g.Node {
	ins := d.Model.Insights
	withData := 0
	for _, i := range ins {
		if i.HasData {
			withData++
		}
	}
	if withData < 2 {
		return ovCard("insights", "Activity insights", "", EmptyState("chart", "Not enough activities yet",
			"Insights need at least two completed activities with glucose data in this range."))
	}
	sc := analytics.Scatter(ins)
	pts := make([]ScatterPoint, len(sc))
	for i, p := range sc {
		pts[i] = ScatterPoint{X: p.X, Y: p.Y, Label: p.Sport + " · " + p.Start.In(d.Loc).Format("2 Jan")}
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
	best, worst := analytics.BestWorst(ins, 3)
	list := func(title string, xs []analytics.ActivityInsight) g.Node {
		items := make([]g.Node, len(xs))
		for i, x := range xs {
			items[i] = Li(Class("flex justify-between gap-3 py-1"),
				A(Href("/activity/"+x.ID), g.Textf("%s · %s", x.Sport, x.Start.In(d.Loc).Format("2 Jan"))),
				Span(Class("num"), g.Textf("%s · %s→%s %s", pct0(x.TIR.InRange),
					render.Value(x.StartGlucose, d.Unit), render.Value(x.EndGlucose, d.Unit), d.Unit)))
		}
		return Div(H3(g.Text(title)), Ul(g.Group(items)))
	}
	return ovCard("insights", "Activity insights", fmt.Sprintf("%d activities with glucose data", withData),
		Grid("",
			StatTile("Low within 3 h after", fmt.Sprintf("%d of %d", postLows, lows), "activities followed by a low", nil),
		),
		Chart("ov-insight-scatter", ScatterOption(ScatterInput{
			Points: pts, XName: "Start glucose", YName: "Change during activity",
			XFormat: glucoseFmt(d.Unit), YFormat: glucoseFmt(d.Unit), Color: colLine,
		}), 300),
		P(Class("muted text-sm"), g.Text("Each dot is one activity. Starting higher usually means a bigger drop; the dots low on the chart are the ones to watch.")),
		Grid("2", list("Best time in range", best), list("Lowest time in range", worst)),
	)
}

// ---- table

func statsTableCard(d StatsData, _ map[string]string) g.Node {
	acts := d.Model.Acts
	return ovCard("table", "Activities", fmt.Sprintf("%d in this range", len(acts)),
		activityTable(DashData{Acts: acts, Unit: d.Unit, Loc: d.Loc, Now: d.Now}))
}

// ---- sources and coverage

const chipClass = "flex items-center gap-2 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm"

func sourcesCard(d StatsData, _ map[string]string) g.Node {
	if len(d.Sources) == 0 {
		return ovCard("sources", "Glucose sources", "", EmptyState("inbox", "No glucose readings stored yet",
			"Connect Dexcom under Settings or import a history."))
	}
	chips := make([]g.Node, 0, len(d.Sources))
	for _, si := range d.Sources {
		chips = append(chips, Div(Class(chipClass), Code(g.Text(si.Source)),
			Span(Class("text-ink-2"), g.Textf("%d readings", si.Count)),
			Span(Class("text-ink-2"), g.Text("newest "+fmtWhen(si.Latest, d.Loc, d.Now)))))
	}
	sub := "When the newest reading from each source arrived, so a stopped connection or a failed import shows up here."
	body := []g.Node{Div(Class("flex flex-wrap gap-2"), g.Group(chips))}
	if c := d.Model.Cur.Coverage; d.Model.HasData {
		line := fmt.Sprintf("Coverage in this range: %.0f%%.", c.Pct)
		if len(c.Gaps) > 0 {
			gaps := append([]analytics.Gap(nil), c.Gaps...)
			sort.Slice(gaps, func(i, j int) bool { return gaps[i].Duration > gaps[j].Duration })
			l := gaps[0]
			line += fmt.Sprintf(" %d gaps over 30 minutes, the longest %s starting %s.", len(gaps), fmtDuration(l.Duration), fmtWhen(l.Start, d.Loc, d.Now))
		} else {
			line += " No gaps over 30 minutes."
		}
		body = append(body, P(Class("muted mt-3 mb-0"), g.Text(line)))
	}
	return ovCard("sources", "Glucose sources", sub, body...)
}

// ---- toolbar

// rangeToolbar is the preset switch, the custom-range form and the compare
// switch. All of it is plain links and a GET form, so it works without
// JavaScript and the address bar always holds the current view.
func rangeToolbar(d StatsData) g.Node {
	r := d.Range
	items := make([]NavItem, 0, len(store.OverviewRanges))
	for _, k := range store.OverviewRanges {
		items = append(items, NavItem{Key: k, Label: statsRangeLabels[k], Href: r.presetHref(k)})
	}
	custom := g.El("details", Class("relative"), g.If(r.Key == "custom", g.Attr("open", "")),
		g.El("summary", Class(customSummaryClass), g.Attr("data-active", boolAttr(r.Key == "custom")), g.Text("Custom range")),
		Form(Method("get"), Action("/stats"), Class(customFormClass),
			Div(Label(g.Text("From"), Input(Type("date"), Name("from"), Required(), g.If(r.Key == "custom", Value(r.FromStr)))),
				Label(g.Text("To"), Input(Type("date"), Name("to"), Required(), g.If(r.Key == "custom", Value(r.ToStr))))),
			g.If(r.Compare, Input(Type("hidden"), Name("compare"), Value("prev"))),
			SubmitBtn("primary", "sm", "Show"),
		),
	)
	cmp := []NavItem{{Key: "off", Label: "No comparison", Href: r.href(false)}, {Key: "prev", Label: "vs previous period", Href: r.href(true)}}
	active := "off"
	if r.Compare {
		active = "prev"
	}
	return Div(Class("mb-4 flex flex-wrap items-center gap-2"),
		Segmented("Date range", items, presetActive(r)), custom,
		g.If(r.Key != "all", Span(Class("ml-auto"), Segmented("Comparison", cmp, active))))
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
func trendSpan(first, last time.Time) string {
	return fmt.Sprintf("%s – %s", first.Format("2 Jan"), last.Format("2 Jan"))
}

// StatsPage is the Overview: the configured cards over a selectable range.
func StatsPage(pd PageData, d StatsData) g.Node {
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
			cards = append(cards, draw(d, c.Options))
		}
	}
	var lead g.Node
	switch {
	case enabled == 0:
		lead = Card(P(Class("muted"), g.Text("Every card on this page is turned off. Turn one back on under Settings → Overview page.")))
	case !d.Model.HasData:
		lead = Card(EmptyState("inbox", "No glucose readings in this range",
			"Pick a longer range, or check that a source is connected under Settings."))
	}
	return Page(pd,
		PageHead("Overview", "Everything glucava has recorded, not just the activities."),
		g.If(d.Range.Note != "", Notice("warning", g.Text(d.Range.Note+" Showing the default range instead."))),
		rangeToolbar(d),
		Div(Class("stack"), lead, g.Group(cards)),
	)
}
