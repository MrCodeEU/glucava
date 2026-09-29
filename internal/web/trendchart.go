package web

import (
	"fmt"
	"strings"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

const (
	trendChartW   = 600.0
	trendChartH   = 140.0
	trendChartPad = 20.0
)

// trendLineChart renders values (one per day, chronological, no gaps
// represented) as a simple inline SVG line, evenly spaced along x by index
// rather than to scale by calendar date. That is enough for a day-by-day
// trend where consecutive points are already one calendar day apart, and
// simpler than laying out visual gaps for days with no activity.
// labelFmt formats a value for the two y-axis labels (min and max); an
// empty or single-point series renders a placeholder instead of a chart.
func trendLineChart(color string, values []float64, labelFmt func(float64) string) g.Node {
	if len(values) == 0 {
		return P(Class("muted"), g.Text("Not enough data yet."))
	}
	lo, hi := values[0], values[0]
	for _, v := range values {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	if hi == lo {
		hi = lo + 1 // avoid a divide by zero; draws as a flat line instead
	}
	x := func(i int) float64 {
		if len(values) == 1 {
			return trendChartW / 2
		}
		return trendChartPad + float64(i)*(trendChartW-2*trendChartPad)/float64(len(values)-1)
	}
	y := func(v float64) float64 {
		return trendChartH - trendChartPad - (v-lo)/(hi-lo)*(trendChartH-2*trendChartPad)
	}
	pts := make([]string, len(values))
	for i, v := range values {
		pts[i] = fmt.Sprintf("%.1f,%.1f", x(i), y(v))
	}
	return g.El("svg", append(comp("trendchart"),
		g.Attr("viewBox", fmt.Sprintf("0 0 %.0f %.0f", trendChartW, trendChartH)),
		g.Attr("preserveAspectRatio", "none"),
		g.Attr("role", "img"),
		g.El("polyline",
			g.Attr("points", strings.Join(pts, " ")),
			g.Attr("fill", "none"), g.Attr("stroke", color), g.Attr("stroke-width", "2"),
			g.Attr("stroke-linejoin", "round"), g.Attr("stroke-linecap", "round"),
		),
		g.El("text", append(comp("trendchart-label"), g.Attr("x", "2"), g.Attr("y", "12"), g.Text(labelFmt(hi)))...),
		g.El("text", append(comp("trendchart-label"), g.Attr("x", "2"), g.Attr("y", fmt.Sprintf("%.0f", trendChartH-4)), g.Text(labelFmt(lo)))...),
	)...)
}
