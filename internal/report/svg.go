package report

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// The band colours match the web UI (internal/web/charts.go); they are
// repeated here because this package must not import internal/web.
const (
	colVeryLow  = "#b91c1c"
	colLow      = "#d55e00"
	colInRange  = "#2a9d6f"
	colHigh     = "#e6b800"
	colVeryHigh = "#a86b00"
	colLine     = "#2563eb"

	colInk    = "#1f2933"
	colMuted  = "#52606d"
	colGrid   = "#d9dee3"
	colTarget = "#d5eee3"
	colBand1  = "#dbe7fb" // 5..95 percentile
	colBand2  = "#a9c4f5" // 25..75 percentile
)

// chartWidth is the shared viewBox width; Typst scales every chart to the
// text width, so 680 units are 170 mm and a 11-unit label is about 7.9 pt.
const chartWidth = 680

// canvas is a tiny SVG writer. Numbers are printed with one decimal so the
// output is stable byte for byte.
type canvas struct {
	b    strings.Builder
	w, h float64
}

func newCanvas(w, h float64) *canvas {
	c := &canvas{w: w, h: h}
	fmt.Fprintf(&c.b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" font-family="Inter" font-size="11">`, w, h, w, h)
	return c
}

func (c *canvas) finish() []byte {
	c.b.WriteString("</svg>")
	return []byte(c.b.String())
}

// num formats a coordinate; NaN and Inf never reach the file.
func num(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		v = 0
	}
	return fmt.Sprintf("%.1f", v)
}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

func (c *canvas) rect(x, y, w, h float64, fill string, extra ...string) {
	if w <= 0 || h <= 0 {
		return
	}
	fmt.Fprintf(&c.b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"%s/>`, num(x), num(y), num(w), num(h), fill, attrs(extra))
}

func (c *canvas) line(x1, y1, x2, y2 float64, stroke string, width float64, extra ...string) {
	fmt.Fprintf(&c.b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="%s"%s/>`,
		num(x1), num(y1), num(x2), num(y2), stroke, num(width), attrs(extra))
}

// text draws s; anchor is start, middle or end.
func (c *canvas) text(x, y float64, s, anchor, fill string, extra ...string) {
	fmt.Fprintf(&c.b, `<text x="%s" y="%s" text-anchor="%s" fill="%s"%s>%s</text>`, num(x), num(y), anchor, fill, attrs(extra), esc(s))
}

type pt struct{ x, y float64 }

func (c *canvas) poly(pts []pt, fill, stroke string, width float64, closed bool) {
	if len(pts) < 2 {
		return
	}
	var d strings.Builder
	for i, p := range pts {
		if i == 0 {
			d.WriteString("M")
		} else {
			d.WriteString("L")
		}
		d.WriteString(num(p.x) + " " + num(p.y))
	}
	if closed {
		d.WriteString("Z")
	}
	fmt.Fprintf(&c.b, `<path d="%s" fill="%s" stroke="%s" stroke-width="%s" stroke-linejoin="round"/>`, d.String(), fill, stroke, num(width))
}

func attrs(extra []string) string {
	if len(extra) == 0 {
		return ""
	}
	return " " + strings.Join(extra, " ")
}

// axis maps a glucose range in mg/dL onto a pixel range and knows the ticks
// in the display unit.
type yAxis struct {
	lo, hi   float64 // mg/dL
	top, bot float64 // pixels
	unit     render.Unit
}

func (a yAxis) y(v float64) float64 {
	v = math.Max(a.lo, math.Min(a.hi, v))
	return a.bot - (v-a.lo)/(a.hi-a.lo)*(a.bot-a.top)
}

// ticks returns mg/dL values with their labels: every 50 mg/dL, or every
// 2 mmol/L.
func (a yAxis) ticks() (vals []float64, labels []string) {
	if a.unit == render.MmolL {
		for t := 2.0; t*stats.MmolFactor <= a.hi; t += 2 {
			if v := t * stats.MmolFactor; v >= a.lo {
				vals, labels = append(vals, v), append(labels, fmt.Sprintf("%.0f", t))
			}
		}
		return
	}
	for t := 50.0; t <= a.hi; t += 50 {
		if t >= a.lo {
			vals, labels = append(vals, t), append(labels, fmt.Sprintf("%.0f", t))
		}
	}
	return
}

// glucoseRange picks the mg/dL span of a chart: at least 40 to 250, wider
// when the data (or the very-high threshold) reaches outside it.
func glucoseRange(thr analytics.Thresholds, maxV float64) (lo, hi float64) {
	lo, hi = 40, math.Max(250, thr.VeryHigh+10)
	if maxV > hi {
		hi = math.Ceil(maxV/50) * 50
	}
	return lo, math.Min(hi, 500)
}

func (a yAxis) drawGrid(c *canvas, left, right float64) {
	vals, labels := a.ticks()
	for i, v := range vals {
		c.line(left, a.y(v), right, a.y(v), colGrid, 0.6)
		c.text(left-6, a.y(v)+3.5, labels[i], "end", colMuted)
	}
}

// AGPSVG draws the ambulatory glucose profile: the median with the 25-75 and
// 5-95 percentile bands over the time of day, and the target range shaded.
func AGPSVG(tr *i18n.Translator, agp analytics.AGP, thr analytics.Thresholds, unit render.Unit) []byte {
	const h, left, right, top, bottom = 172.0, 38.0, 10.0, 8.0, 24.0
	c := newCanvas(chartWidth, h)
	maxV := 0.0
	for _, b := range agp.Bins {
		if !b.Sparse && b.N > 0 {
			maxV = math.Max(maxV, b.P95)
		}
	}
	lo, hi := glucoseRange(thr, maxV)
	ax := yAxis{lo: lo, hi: hi, top: top, bot: h - bottom, unit: unit}
	pw := chartWidth - left - right
	xOf := func(min float64) float64 { return left + min/1440*pw }

	c.rect(left, ax.y(thr.High), pw, ax.y(thr.Low)-ax.y(thr.High), colTarget)
	ax.drawGrid(c, left, chartWidth-right)
	for hr := 0; hr <= 24; hr += 3 {
		x := xOf(float64(hr * 60))
		c.line(x, top, x, h-bottom, colGrid, 0.6)
		anchor := "middle"
		if hr == 24 {
			anchor = "end"
		}
		c.text(x, h-bottom+15, fmt.Sprintf("%02d:00", hr%24), anchor, colMuted)
	}
	c.line(left, ax.y(thr.Low), chartWidth-right, ax.y(thr.Low), colInRange, 0.8, `stroke-dasharray="3 3"`)
	c.line(left, ax.y(thr.High), chartWidth-right, ax.y(thr.High), colInRange, 0.8, `stroke-dasharray="3 3"`)

	// Runs of usable bins; a sparse or empty bin breaks the band.
	var runs [][]analytics.AGPBin
	var cur []analytics.AGPBin
	for _, b := range agp.Bins {
		if b.N > 0 && !b.Sparse {
			cur = append(cur, b)
			continue
		}
		if len(cur) > 0 {
			runs = append(runs, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}
	if len(runs) == 0 {
		c.text(chartWidth/2, h/2, tr.T("report.chart.noprofile"), "middle", colMuted)
		return c.finish()
	}
	band := func(lo, hi func(analytics.AGPBin) float64, fill string) {
		for _, run := range runs {
			var pts []pt
			for _, b := range run {
				pts = append(pts, pt{xOf(float64(b.Minute) + 7.5), ax.y(hi(b))})
			}
			for i := len(run) - 1; i >= 0; i-- {
				pts = append(pts, pt{xOf(float64(run[i].Minute) + 7.5), ax.y(lo(run[i]))})
			}
			c.poly(pts, fill, "none", 0, true)
		}
	}
	band(func(b analytics.AGPBin) float64 { return b.P5 }, func(b analytics.AGPBin) float64 { return b.P95 }, colBand1)
	band(func(b analytics.AGPBin) float64 { return b.P25 }, func(b analytics.AGPBin) float64 { return b.P75 }, colBand2)
	for _, run := range runs {
		var pts []pt
		for _, b := range run {
			pts = append(pts, pt{xOf(float64(b.Minute) + 7.5), ax.y(b.P50)})
		}
		c.poly(pts, "none", colLine, 2, false)
	}
	return c.finish()
}

// TIRBarSVG draws the five-band split as one stacked bar.
func TIRBarSVG(t analytics.TIR5) []byte {
	const h, barY, barH = 36.0, 3.0, 30.0
	c := newCanvas(chartWidth, h)
	segs := []struct {
		v    float64
		col  string
		text string
	}{
		{t.VeryLow, colVeryLow, "#ffffff"}, {t.Low, colLow, "#ffffff"}, {t.InRange, colInRange, "#ffffff"},
		{t.High, colHigh, colInk}, {t.VeryHigh, colVeryHigh, "#ffffff"},
	}
	total := 0.0
	for _, s := range segs {
		total += s.v
	}
	if total <= 0 {
		c.rect(0, barY, chartWidth, barH, colGrid)
		return c.finish()
	}
	x := 0.0
	for _, s := range segs {
		w := s.v / total * chartWidth
		if w <= 0 {
			continue
		}
		c.rect(x, barY, w, barH, s.col)
		if w >= 34 {
			c.text(x+w/2, barY+barH/2+4, fmt.Sprintf("%.0f%%", s.v), "middle", s.text, `font-weight="700"`)
		}
		x += w
	}
	return c.finish()
}

// TrendSVG draws the day-by-day picture: average glucose with the daily
// min-max band on top, time in range as bars below.
func TrendSVG(tr *i18n.Translator, days []analytics.Day, thr analytics.Thresholds, unit render.Unit, loc *time.Location) []byte {
	const h, left, right = 182.0, 38.0, 10.0
	c := newCanvas(chartWidth, h)
	if len(days) == 0 {
		c.text(chartWidth/2, h/2, tr.T("report.chart.nodays"), "middle", colMuted)
		return c.finish()
	}
	pw := chartWidth - left - right
	first, last := days[0].Date, days[len(days)-1].Date
	span := math.Max(1, last.Sub(first).Hours()/24)
	xOf := func(t time.Time) float64 { return left + t.Sub(first).Hours()/24/span*pw }

	maxV := 0.0
	for _, d := range days {
		maxV = math.Max(maxV, d.Max)
	}
	lo, hi := glucoseRange(thr, maxV)
	ax := yAxis{lo: lo, hi: hi, top: 8, bot: 100, unit: unit}
	c.rect(left, ax.y(thr.High), pw, ax.y(thr.Low)-ax.y(thr.High), colTarget)
	ax.drawGrid(c, left, chartWidth-right)

	var top, bot, avg []pt
	for _, d := range days {
		x := xOf(d.Date)
		top = append(top, pt{x, ax.y(d.Max)})
		bot = append(bot, pt{x, ax.y(d.Min)})
		avg = append(avg, pt{x, ax.y(d.Avg)})
	}
	if len(days) > 1 {
		band := append([]pt{}, top...)
		for i := len(bot) - 1; i >= 0; i-- {
			band = append(band, bot[i])
		}
		c.poly(band, colBand1, "none", 0, true)
		c.poly(avg, "none", colLine, 2, false)
	} else {
		c.rect(top[0].x-3, top[0].y, 6, math.Max(1, bot[0].y-top[0].y), colBand2)
		c.rect(avg[0].x-3, avg[0].y-3, 6, 6, colLine)
	}

	// Time in range: one bar per day, 0-100%.
	tTop, tBot := 126.0, 160.0
	for _, p := range []float64{0, 50, 100} {
		y := tBot - p/100*(tBot-tTop)
		c.line(left, y, chartWidth-right, y, colGrid, 0.6)
		c.text(left-6, y+3.5, fmt.Sprintf("%.0f%%", p), "end", colMuted)
	}
	c.text(left, tTop-6, tr.T("report.label.timeInRange"), "start", colMuted, `font-size="10"`)
	bw := math.Max(1.2, math.Min(14, pw/(span+1)*0.7))
	for _, d := range days {
		v := d.TIR.InRange
		col := colInRange
		if v < 70 {
			col = colHigh
		}
		if v < 50 {
			col = colLow
		}
		hh := v / 100 * (tBot - tTop)
		c.rect(xOf(d.Date)-bw/2, tBot-hh, bw, hh, col)
	}

	// X labels: about six dates, always including the first.
	n := 6
	if len(days) < n {
		n = len(days)
	}
	long := last.Sub(first) > 300*24*time.Hour
	for i := 0; i < n; i++ {
		t := first
		if n > 1 {
			t = first.Add(time.Duration(float64(last.Sub(first)) * float64(i) / float64(n-1)))
		}
		label := tr.Date(t.In(loc), false)
		if long {
			label = tr.Month(t.In(loc).Month(), false) + " " + strconv.Itoa(t.In(loc).Year())
		}
		anchor := "middle"
		switch {
		case i == 0 && n > 1:
			anchor = "start"
		case i == n-1 && n > 1:
			anchor = "end"
		}
		x := xOf(t)
		c.line(x, tBot, x, tBot+3, colMuted, 0.8)
		c.text(x, tBot+15, label, anchor, colMuted)
	}
	return c.finish()
}

// DayPartsSVG draws one stacked five-band bar per part of the day.
func DayPartsSVG(tr *i18n.Translator, parts [4]analytics.DayPart) []byte {
	const rowH, gap, left, right = 17.0, 8.0, 118.0, 56.0
	h := 4*rowH + 3*gap + 6
	c := newCanvas(chartWidth, h)
	bw := chartWidth - left - right
	for i, p := range parts {
		y := 3 + float64(i)*(rowH+gap)
		c.text(left-8, y+rowH/2+4, partLabel(tr, i, p), "end", colInk)
		if p.Count == 0 {
			c.rect(left, y, bw, rowH, "#eef1f4")
			c.text(left+8, y+rowH/2+4, tr.T("report.chart.noreadings"), "start", colMuted, `font-size="10"`)
			continue
		}
		x := left
		for _, s := range []struct {
			v   float64
			col string
		}{{p.TIR.VeryLow, colVeryLow}, {p.TIR.Low, colLow}, {p.TIR.InRange, colInRange}, {p.TIR.High, colHigh}, {p.TIR.VeryHigh, colVeryHigh}} {
			w := s.v / 100 * bw
			c.rect(x, y, w, rowH, s.col)
			x += w
		}
		c.text(chartWidth-right+8, y+rowH/2+4, fmt.Sprintf("%.0f%%", p.TIR.InRange), "start", colInk, `font-weight="700"`)
	}
	return c.finish()
}
