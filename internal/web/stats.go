package web

import (
	"fmt"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/overview"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/store"
)

// statsRanges are the date-range picker's options, in order, matching the
// ?range= query param values statsRangeBounds accepts.
var statsRanges = []struct{ key, label string }{
	{"7d", "7 days"}, {"30d", "30 days"}, {"90d", "90 days"}, {"all", "All time"},
}

// statsRangeBounds returns the [from, to] window for range (an empty or
// unrecognised value falls back to "30d") and the range key actually used.
func statsRangeBounds(rangeParam string, now time.Time) (key string, from, to time.Time) {
	to = now
	switch rangeParam {
	case "7d":
		return "7d", now.AddDate(0, 0, -7), to
	case "90d":
		return "90d", now.AddDate(0, 0, -90), to
	case "all":
		return "all", now.AddDate(-10, 0, 0), to // far enough back to include everything real
	default:
		return "30d", now.AddDate(0, 0, -30), to
	}
}

// StatsData feeds the stats overview page.
type StatsData struct {
	Range        string // the active range key ("7d", "30d", "90d", "all")
	Overview     overview.Data
	General      overview.GeneralData
	SourceHealth []store.SourceInfo
	Unit         render.Unit
	Loc          *time.Location
	Now          time.Time

	ShowTrend        bool
	ShowBySport      bool
	ShowTable        bool
	ShowGeneral      bool
	ShowSourceHealth bool
}

func rangePicker(active string) g.Node {
	links := make([]g.Node, 0, len(statsRanges))
	for _, r := range statsRanges {
		links = append(links, A(Href("/stats?range="+r.key), g.Text(r.label),
			g.If(r.key == active, g.Attr("aria-current", "page"))))
	}
	return Div(append(comp("rangepicker"), g.Group(links))...)
}

func trendCard(d StatsData) g.Node {
	pts := d.Overview.Trend
	if len(pts) == 0 {
		return Card(H2(g.Text("Trends")), P(Class("muted"), g.Text("No completed activities in this range yet.")))
	}
	tir, avg := make([]float64, len(pts)), make([]float64, len(pts))
	for i, p := range pts {
		tir[i], avg[i] = p.TIR, p.Avg
	}
	first, last := pts[0].Day, pts[len(pts)-1].Day
	span := fmt.Sprintf("%s – %s", first.Format("2 Jan"), last.Format("2 Jan"))
	return Card(
		H2(g.Text("Trends")), P(Class("muted"), g.Text(span+", one point per day with a completed activity")),
		Grid("2",
			Div(H3(g.Text("Time in range")),
				trendLineChart("var(--ok)", tir, func(v float64) string { return fmt.Sprintf("%.0f%%", v) })),
			Div(H3(g.Text("Average glucose")),
				trendLineChart("var(--info)", avg, func(v float64) string { return render.Value(v, d.Unit) + " " + string(d.Unit) })),
		),
	)
}

func bySportCard(d StatsData) g.Node {
	sports := d.Overview.BySport
	if len(sports) == 0 {
		return Card(H2(g.Text("By activity type")), P(Class("muted"), g.Text("No completed activities in this range yet.")))
	}
	max := sports[0].Count
	rows := make([]g.Node, 0, len(sports))
	for _, s := range sports {
		pct := 100.0
		if max > 0 {
			pct = float64(s.Count) / float64(max) * 100
		}
		rows = append(rows, Div(append(comp("sportbar"),
			Span(Class("name"), g.Text(s.Sport)),
			Div(Class("track"), Span(g.Attr("style", fmt.Sprintf("width:%.1f%%", pct)))),
			Span(Class("val"), g.Textf("%.0f%% TIR · %d×", s.AvgTIR, s.Count)),
		)...))
	}
	return Card(H2(g.Text("By activity type")), g.Group(rows))
}

// generalCard shows the whole-range glucose picture, independent of any
// activity: every stored reading in the range, not just the ones around a
// workout. This also acts as the simplest possible check that a source (the
// live connection or a backfill import) actually put data in the database.
func generalCard(d StatsData) g.Node {
	g2 := d.General
	if !g2.HasData {
		return Card(H2(g.Text("General glucose")), P(Class("muted"), g.Text("No glucose readings stored in this range yet.")))
	}
	pts := g2.Trend
	tir, avg := make([]float64, len(pts)), make([]float64, len(pts))
	for i, p := range pts {
		tir[i], avg[i] = p.TIR, p.Avg
	}
	s := g2.Overall
	return Card(
		H2(g.Text("General glucose")),
		P(Class("muted"), g.Text("Every stored reading in this range, not just the ones around an activity.")),
		Grid("",
			Tile("Time in range", fmt.Sprintf("%.0f%%", s.TIR), fmt.Sprintf("%.0f%% below · %.0f%% above", s.Below, s.Above)),
			Tile("Average", render.Value(s.Avg, d.Unit), string(d.Unit)),
			Tile("Min / Max", render.Value(s.Min, d.Unit)+" / "+render.Value(s.Max, d.Unit), string(d.Unit)),
			Tile("Readings", fmt.Sprint(s.Count), "in this range"),
		),
		g.If(len(pts) > 0, Grid("2",
			Div(H3(g.Text("Time in range")),
				trendLineChart("var(--ok)", tir, func(v float64) string { return fmt.Sprintf("%.0f%%", v) })),
			Div(H3(g.Text("Average glucose")),
				trendLineChart("var(--info)", avg, func(v float64) string { return render.Value(v, d.Unit) + " " + string(d.Unit) })),
		)),
	)
}

// sourceHealthCard lists each glucose source (the live connection, and any
// backfill import) with its reading count and how long ago the newest one
// arrived — the signal that a live source is still connected, or that an
// import actually landed something, without waiting for an activity to show
// up or not.
func sourceHealthCard(d StatsData) g.Node {
	if len(d.SourceHealth) == 0 {
		return Card(H2(g.Text("Glucose sources")), P(Class("muted"), g.Text("No glucose readings stored yet.")))
	}
	rows := make([]g.Node, 0, len(d.SourceHealth))
	for _, si := range d.SourceHealth {
		rows = append(rows, Tr(
			Td(Code(g.Text(si.Source))),
			Td(Class("num"), g.Textf("%d", si.Count)),
			Td(g.Text(fmtWhen(si.Latest, d.Loc, d.Now))),
		))
	}
	return Card(H2(g.Text("Glucose sources")),
		P(Class("muted"), g.Text("When the newest reading from each source arrived, so a stopped live connection or a failed import shows up here.")),
		Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text("Source")), Th(Class("num"), g.Text("Readings")), Th(g.Text("Newest reading")))),
			TBody(g.Group(rows)))...))...),
	)
}

func statsTableCard(d StatsData) g.Node {
	acts := d.Overview.Activities
	return Card(H2(g.Text("Activities")), activityTable(DashData{Acts: acts, Unit: d.Unit, Loc: d.Loc, Now: d.Now}))
}

// StatsPage is the overview/stats page: trends, a per-activity-type
// breakdown and a raw table, over a selectable date range.
func StatsPage(pd PageData, d StatsData) g.Node {
	var cards []g.Node
	if d.ShowGeneral {
		cards = append(cards, generalCard(d))
	}
	if d.ShowSourceHealth {
		cards = append(cards, sourceHealthCard(d))
	}
	if d.ShowTrend {
		cards = append(cards, trendCard(d))
	}
	if d.ShowBySport {
		cards = append(cards, bySportCard(d))
	}
	if d.ShowTable {
		cards = append(cards, statsTableCard(d))
	}
	if len(cards) == 0 {
		cards = append(cards, Card(P(Class("muted"), g.Text("Every card on this page is turned off. Turn one back on under Settings → Overview page."))))
	}
	return Page(pd,
		PageHead("Overview", "Trends across your activities, not just one at a time.", rangePicker(d.Range)),
		Div(Class("stack"), g.Group(cards)),
	)
}
