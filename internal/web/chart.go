package web

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// ChartData is what GlucoseChart draws. Values are mg/dL.
type ChartData struct {
	Samples    []stats.Sample
	Range      stats.Range
	Unit       render.Unit
	Start, End time.Time // the activity, shaded on the chart
	Loc        *time.Location
	T          *i18n.Translator // nil means English
}

const (
	chW, chH               = 720.0, 260.0
	padL, padR, padT, padB = 44.0, 14.0, 12.0, 28.0
)

// GlucoseChart draws a glucose curve as inline SVG: target band, activity span,
// and out-of-range points. Colours come from CSS variables, so it follows the theme.
func GlucoseChart(d ChartData) g.Node {
	tr := orEnglish(d.T)
	if len(d.Samples) == 0 {
		return Div(append(comp("empty"), g.Text(tr.T("chart.svg.empty")))...)
	}
	pts := append([]stats.Sample(nil), d.Samples...)
	sort.Slice(pts, func(i, j int) bool { return pts[i].Time.Before(pts[j].Time) })

	t0, t1 := pts[0].Time, pts[len(pts)-1].Time
	if d.Start.Before(t0) && !d.Start.IsZero() {
		t0 = d.Start
	}
	if d.End.After(t1) {
		t1 = d.End
	}
	if !t1.After(t0) {
		t1 = t0.Add(time.Minute)
	}

	lo, hi := math.Min(40, d.Range.Low-10), math.Max(250, d.Range.High+30)
	for _, p := range pts {
		lo, hi = math.Min(lo, p.Value-10), math.Max(hi, p.Value+10)
	}

	x := func(t time.Time) float64 {
		return padL + (chW-padL-padR)*float64(t.Sub(t0))/float64(t1.Sub(t0))
	}
	y := func(v float64) float64 { return padT + (chH-padT-padB)*(1-(v-lo)/(hi-lo)) }

	var path strings.Builder
	for i, p := range pts {
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		fmt.Fprintf(&path, "%s%.1f %.1f ", cmd, x(p.Time), y(p.Value))
	}

	nodes := []g.Node{
		g.Attr("viewBox", fmt.Sprintf("0 0 %.0f %.0f", chW, chH)),
		g.Attr("role", "img"),
		g.Attr("aria-label", tr.T("chart.svg.aria", "from", tr.Time(pts[0].Time.In(d.Loc)), "to", tr.Time(pts[len(pts)-1].Time.In(d.Loc)))),
		g.El("rect", Class("band"), g.Attr("x", f1(padL)), g.Attr("y", f1(y(d.Range.High))),
			g.Attr("width", f1(chW-padL-padR)), g.Attr("height", f1(y(d.Range.Low)-y(d.Range.High)))),
	}
	if !d.Start.IsZero() && d.End.After(d.Start) {
		nodes = append(nodes, g.El("rect", Class("span"), g.Attr("x", f1(x(d.Start))), g.Attr("y", f1(padT)),
			g.Attr("width", f1(x(d.End)-x(d.Start))), g.Attr("height", f1(chH-padT-padB))))
	}

	// Horizontal gridlines at the range limits and round values.
	for _, v := range gridValues(lo, hi, d.Range) {
		cls := "grid"
		if v == d.Range.Low || v == d.Range.High {
			cls = "limit"
		}
		nodes = append(nodes,
			g.El("line", Class(cls), g.Attr("x1", f1(padL)), g.Attr("x2", f1(chW-padR)), g.Attr("y1", f1(y(v))), g.Attr("y2", f1(y(v)))),
			g.El("text", g.Attr("x", f1(padL-6)), g.Attr("y", f1(y(v)+4)), g.Attr("text-anchor", "end"), g.Text(valueT(tr, v, d.Unit))),
		)
	}
	// Time labels every 15/30/60 minutes depending on the span.
	for _, t := range timeTicks(t0, t1, d.Loc) {
		nodes = append(nodes, g.El("text", g.Attr("x", f1(x(t))), g.Attr("y", f1(chH-8)), g.Attr("text-anchor", "middle"), g.Text(tr.Time(t.In(d.Loc)))))
	}

	nodes = append(nodes, g.El("path", Class("line"), g.Attr("d", strings.TrimSpace(path.String()))))
	for _, p := range pts {
		cls := ""
		switch {
		case p.Value < d.Range.Low:
			cls = "dot-low"
		case p.Value > d.Range.High:
			cls = "dot-high"
		}
		if cls != "" {
			nodes = append(nodes, g.El("circle", Class(cls), g.Attr("cx", f1(x(p.Time))), g.Attr("cy", f1(y(p.Value))), g.Attr("r", "3")))
		}
	}

	return Div(
		g.El("svg", append(comp("chart"), nodes...)...),
		Div(append(comp("legend"),
			Span(g.Raw(`<i style="background:var(--band)"></i>`), g.Text(tr.T("chart.svg.target", "low", valueT(tr, d.Range.Low, d.Unit), "high", valueT(tr, d.Range.High, d.Unit), "unit", string(d.Unit)))),
			Span(g.Raw(`<i style="background:var(--span)"></i>`), g.Text(tr.T("chart.activity"))),
			Span(g.Raw(`<i style="background:var(--below)"></i>`), g.Text(tr.T("band.below"))),
			Span(g.Raw(`<i style="background:var(--above)"></i>`), g.Text(tr.T("band.above"))),
		)...),
	)
}

func f1(v float64) string { return fmt.Sprintf("%.1f", v) }

// gridValues returns the range limits plus round steps of 50 mg/dL within [lo, hi].
func gridValues(lo, hi float64, r stats.Range) []float64 {
	out := []float64{r.Low, r.High}
	for v := math.Ceil(lo/50) * 50; v <= hi; v += 50 {
		if math.Abs(v-r.Low) > 12 && math.Abs(v-r.High) > 12 {
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out
}

func timeTicks(t0, t1 time.Time, loc *time.Location) []time.Time {
	span := t1.Sub(t0)
	step := 15 * time.Minute
	switch {
	case span > 6*time.Hour:
		step = time.Hour
	case span > 3*time.Hour:
		step = 30 * time.Minute
	}
	var out []time.Time
	for t := t0.In(loc).Truncate(step).Add(step); t.Before(t1); t = t.Add(step) {
		out = append(out, t)
	}
	return out
}
