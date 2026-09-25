package chartimg

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
	"sync"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// HRPoint is one heart rate reading.
type HRPoint = stats.HRSample

// PhotoData is what Photo draws: a square card for the Strava feed, which
// crops every photo to a square.
type PhotoData struct {
	Samples    []stats.Sample
	HR         []HRPoint // optional; drawn on a second axis
	Range      stats.Range
	Summary    *stats.Summary // computed from Samples when nil
	Start, End time.Time      // the activity, shaded
	Unit       render.Unit
	Loc        *time.Location
	Style      Style
}

// logical layout size; everything below is in these units.
const lg = 1000.0

var (
	fontOnce  sync.Once
	fontReg   *opentype.Font
	fontBold  *opentype.Font
	fontErr   error
	faceMu    sync.Mutex
	faceCache = map[string]font.Face{}
)

func face(bold bool, px float64) (font.Face, error) {
	fontOnce.Do(func() {
		if fontReg, fontErr = opentype.Parse(goregular.TTF); fontErr != nil {
			return
		}
		fontBold, fontErr = opentype.Parse(gobold.TTF)
	})
	if fontErr != nil {
		return nil, fontErr
	}
	key := fmt.Sprintf("%v/%.1f", bold, px)
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[key]; ok {
		return f, nil
	}
	src := fontReg
	if bold {
		src = fontBold
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, err
	}
	faceCache[key] = f
	return f, nil
}

// pcanvas draws shapes in logical units at a supersampled resolution.
type pcanvas struct {
	img *image.RGBA
	s   float64 // canvas pixels per logical unit
}

func newPCanvas(px, ss int, bg color.RGBA) *pcanvas {
	c := &pcanvas{image.NewRGBA(image.Rect(0, 0, px*ss, px*ss)), float64(px*ss) / lg}
	for i := 0; i < len(c.img.Pix); i += 4 {
		c.img.Pix[i], c.img.Pix[i+1], c.img.Pix[i+2], c.img.Pix[i+3] = bg.R, bg.G, bg.B, 255
	}
	return c
}

func blend(dst, src color.RGBA, a float64) color.RGBA {
	m := func(d, s uint8) uint8 { return uint8(float64(d)*(1-a) + float64(s)*a + 0.5) }
	return color.RGBA{m(dst.R, src.R), m(dst.G, src.G), m(dst.B, src.B), 255}
}

func (c *pcanvas) rectA(x0, y0, x1, y1 float64, col color.RGBA, a float64) {
	r := image.Rect(int(math.Round(x0*c.s)), int(math.Round(y0*c.s)), int(math.Round(x1*c.s)), int(math.Round(y1*c.s))).Intersect(c.img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if a >= 1 {
				c.img.SetRGBA(x, y, col)
			} else {
				c.img.SetRGBA(x, y, blend(c.img.RGBAAt(x, y), col, a))
			}
		}
	}
}

func (c *pcanvas) rect(x0, y0, x1, y1 float64, col color.RGBA) { c.rectA(x0, y0, x1, y1, col, 1) }

func (c *pcanvas) disc(cx, cy, r float64, col color.RGBA) {
	cx, cy, r = cx*c.s, cy*c.s, r*c.s
	for y := int(cy - r); y <= int(cy+r); y++ {
		for x := int(cx - r); x <= int(cx+r); x++ {
			if (float64(x)-cx)*(float64(x)-cx)+(float64(y)-cy)*(float64(y)-cy) <= r*r && image.Pt(x, y).In(c.img.Bounds()) {
				c.img.SetRGBA(x, y, col)
			}
		}
	}
}

func (c *pcanvas) line(x0, y0, x1, y1, w float64, col color.RGBA) {
	steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))*c.s/1.5) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		c.disc(x0+(x1-x0)*t, y0+(y1-y0)*t, w/2, col)
	}
}

func (c *pcanvas) rrect(x0, y0, x1, y1, r float64, col color.RGBA) {
	c.rect(x0+r, y0, x1-r, y1, col)
	c.rect(x0, y0+r, x1, y1-r, col)
	for _, p := range [][2]float64{{x0 + r, y0 + r}, {x1 - r, y0 + r}, {x0 + r, y1 - r}, {x1 - r, y1 - r}} {
		c.disc(p[0], p[1], r, col)
	}
}

// areaUnder fills between a polyline and the bottom with a translucent colour.
func (c *pcanvas) areaUnder(xs, ys []float64, bottom float64, col color.RGBA, a float64) {
	for i := 1; i < len(xs); i++ {
		px0, px1 := int(xs[i-1]*c.s), int(xs[i]*c.s)
		for px := px0; px < px1; px++ {
			t := 0.0
			if px1 > px0 {
				t = float64(px-px0) / float64(px1-px0)
			}
			y := (ys[i-1] + (ys[i]-ys[i-1])*t) * c.s
			for py := int(y); py < int(bottom*c.s); py++ {
				if image.Pt(px, py).In(c.img.Bounds()) {
					c.img.SetRGBA(px, py, blend(c.img.RGBAAt(px, py), col, a))
				}
			}
		}
	}
}

// photoTheme adds the colours the card needs beyond the email chart's.
type photoTheme struct {
	palette
	tile, muted, strong, hr color.RGBA
}

func themeFor(st Style) photoTheme {
	if st.Dark {
		return photoTheme{darkPalette, color.RGBA{33, 40, 50, 255}, color.RGBA{139, 148, 160, 255}, color.RGBA{240, 244, 248, 255}, color.RGBA{255, 112, 128, 255}}
	}
	return photoTheme{lightPalette, color.RGBA{242, 245, 249, 255}, color.RGBA{104, 113, 126, 255}, color.RGBA{26, 32, 44, 255}, color.RGBA{214, 40, 70, 255}}
}

type label struct {
	x, y  float64 // logical; y is the baseline
	s     string
	px    float64 // logical size
	bold  bool
	col   color.RGBA
	align int // 0 left, 1 center, 2 right
}

// smooth returns points along a Catmull-Rom curve through pts (in logical x/y).
func smooth(xs, ys []float64) (ox, oy []float64) {
	n := len(xs)
	if n < 3 {
		return xs, ys
	}
	at := func(a []float64, i int) float64 { return a[max(0, min(n-1, i))] }
	for i := 0; i < n-1; i++ {
		for k := 0; k < 6; k++ {
			t := float64(k) / 6
			f := func(a []float64) float64 {
				p0, p1, p2, p3 := at(a, i-1), at(a, i), at(a, i+1), at(a, i+2)
				return 0.5 * ((2 * p1) + (-p0+p2)*t + (2*p0-5*p1+4*p2-p3)*t*t + (-p0+3*p1-3*p2+p3)*t*t*t)
			}
			ox, oy = append(ox, f(xs)), append(oy, f(ys))
		}
	}
	return append(ox, xs[n-1]), append(oy, ys[n-1])
}

// Photo draws the square glucose card. Size is 1080 px, or 1620 with Style.Large.
func Photo(d PhotoData) ([]byte, error) {
	if len(d.Samples) == 0 {
		return nil, errors.New("chartimg: no samples")
	}
	if d.Loc == nil {
		d.Loc = time.Local
	}
	th := themeFor(d.Style)
	px, ss := 1080, 2
	if d.Style.Large {
		px = 1620
	}
	pts := append([]stats.Sample(nil), d.Samples...)
	sort.Slice(pts, func(i, j int) bool { return pts[i].Time.Before(pts[j].Time) })
	sum := d.Summary
	if sum == nil {
		if s, ok := stats.Summarize(pts, d.Range); ok {
			sum = &s
		}
	}

	t0, t1 := pts[0].Time, pts[len(pts)-1].Time
	if !d.Start.IsZero() && d.Start.Before(t0) {
		t0 = d.Start
	}
	if d.End.After(t1) {
		t1 = d.End
	}
	if !t1.After(t0) {
		t1 = t0.Add(time.Minute)
	}
	lo, hi := math.Min(50, d.Range.Low-15), math.Max(220, d.Range.High+30)
	for _, p := range pts {
		lo, hi = math.Min(lo, p.Value-10), math.Max(hi, p.Value+10)
	}
	hasHR := len(d.HR) > 1 && !d.Style.HideHR
	hrLo, hrHi := 0.0, 0.0
	if hasHR {
		hrLo, hrHi = math.Inf(1), math.Inf(-1)
		for _, h := range d.HR {
			hrLo, hrHi = math.Min(hrLo, h.BPM), math.Max(hrHi, h.BPM)
		}
		hrLo, hrHi = math.Floor(hrLo/10)*10-10, math.Ceil(hrHi/10)*10+10
	}

	const mx = 60.0
	cl, cr := 130.0, lg-mx // chart box
	if hasHR {
		cr = lg - mx - 74
	}
	ct, cb := 318.0, 690.0
	x := func(t time.Time) float64 { return cl + (cr-cl)*float64(t.Sub(t0))/float64(t1.Sub(t0)) }
	y := func(v float64) float64 { return ct + (cb-ct)*(1-(v-lo)/(hi-lo)) }
	yhr := func(v float64) float64 { return ct + (cb-ct)*(1-(v-hrLo)/(hrHi-hrLo)) }

	c := newPCanvas(px, ss, th.bg)
	var labels []label

	// Header.
	lw := 2.0
	if d.Style.LineWidth > 0 {
		lw = d.Style.LineWidth
	}
	lw *= 2.6 // the email chart's 1-4 scale, in card units
	labels = append(labels,
		label{mx, 96, "GLUCOSE", 28, true, th.muted, 0},
		label{lg - mx, 96, when(d, t0, t1), 28, false, th.muted, 2},
	)
	if sum != nil {
		numW := 0.0
		if f, err := face(true, 150); err == nil { // width of the big number, so the caption never overlaps it
			numW = float64((&font.Drawer{Face: f}).MeasureString(fmt.Sprintf("%.0f%%", sum.TIR)).Round())
		}
		labels = append(labels,
			label{mx - 4, 238, fmt.Sprintf("%.0f%%", sum.TIR), 150, true, th.strong, 0},
			label{mx + numW + 36, 190, "time in range", 34, true, th.strong, 0},
			label{mx + numW + 36, 232, fmt.Sprintf("target %s-%s %s", render.Value(d.Range.Low, d.Unit), render.Value(d.Range.High, d.Unit), d.Unit), 28, false, th.muted, 0},
		)
	}

	// Grid, band, activity.
	if !d.Start.IsZero() && d.End.After(d.Start) && !d.Style.HideActivity {
		c.rect(x(d.Start), ct, x(d.End), cb, th.span)
	}
	if !d.Style.HideBand {
		c.rect(cl, y(d.Range.High), cr, y(d.Range.Low), th.band)
	}
	for _, v := range yTicks(lo, hi, d.Range, d.Unit) {
		w := 1.5
		if v == d.Range.Low || v == d.Range.High {
			w = 2.5
		}
		c.rect(cl, y(v)-w/2, cr, y(v)+w/2, th.grid)
		labels = append(labels, label{cl - 16, y(v) + 9, render.Value(v, d.Unit), 26, v == d.Range.Low || v == d.Range.High, th.muted, 2})
	}
	c.rect(cl, cb, cr, cb+2.5, th.grid)
	for _, t := range xTicks(t0, t1, d.Loc, (cr-cl)/110) {
		c.rect(x(t)-1, cb, x(t)+1, cb+10, th.grid)
		labels = append(labels, label{x(t), cb + 44, t.In(d.Loc).Format("15:04"), 26, false, th.muted, 1})
	}

	// Heart rate underneath the glucose curve, on its own axis.
	if hasHR {
		hs := append([]HRPoint(nil), d.HR...)
		sort.Slice(hs, func(i, j int) bool { return hs[i].Time.Before(hs[j].Time) })
		var xs, ys []float64
		for _, h := range hs {
			if h.Time.Before(t0) || h.Time.After(t1) {
				continue
			}
			xs, ys = append(xs, x(h.Time)), append(ys, yhr(h.BPM))
		}
		if len(xs) > 1 {
			sx, sy := smooth(xs, ys)
			for i := 1; i < len(sx); i++ {
				c.line(sx[i-1], sy[i-1], sx[i], sy[i], 4.5, th.hr)
			}
			labels = append(labels,
				label{cr + 14, ct + 8, fmt.Sprintf("%.0f", hrHi), 24, false, th.hr, 0},
				label{cr + 14, cb + 2, fmt.Sprintf("%.0f", hrLo), 24, false, th.hr, 0},
				label{cr + 14, (ct+cb)/2 + 8, "bpm", 24, true, th.hr, 0},
			)
		}
	}

	// Glucose curve: soft area, then the line coloured by where it is.
	var xs, ys []float64
	for _, p := range pts {
		xs, ys = append(xs, x(p.Time)), append(ys, y(p.Value))
	}
	sx, sy := smooth(xs, ys)
	c.areaUnder(sx, sy, cb, th.line, 0.10)
	valAt := func(yy float64) float64 { return lo + (hi-lo)*(1-(yy-ct)/(cb-ct)) }
	for i := 1; i < len(sx); i++ {
		col := th.line
		if !d.Style.HideDots { // the colouring is the "mark out-of-range" option
			switch v := valAt((sy[i-1] + sy[i]) / 2); {
			case v < d.Range.Low:
				col = colLow
			case v > d.Range.High:
				col = colHigh
			}
		}
		c.line(sx[i-1], sy[i-1], sx[i], sy[i], lw, col)
	}

	// Time in range bar and the three numbers.
	if sum != nil {
		by, bh := 748.0, 30.0
		segs := []struct {
			v   float64
			col color.RGBA
			s   string
		}{{sum.Below, colLow, "below"}, {sum.TIR, colInBar, "in range"}, {sum.Above, colHigh, "above"}}
		bx := mx
		for _, sg := range segs {
			w := (lg - 2*mx) * math.Max(0, sg.v) / 100
			c.rect(bx, by, bx+w, by+bh, sg.col)
			bx += w
		}
		labels = append(labels,
			label{mx, by + bh + 38, fmt.Sprintf("%.0f%% below", sum.Below), 26, false, th.muted, 0},
			label{lg / 2, by + bh + 38, fmt.Sprintf("%.0f%% in range", sum.TIR), 26, false, th.muted, 1},
			label{lg - mx, by + bh + 38, fmt.Sprintf("%.0f%% above", sum.Above), 26, false, th.muted, 2},
		)
		tw := (lg - 2*mx - 2*24) / 3
		for i, st := range []struct{ name, val string }{
			{"lowest", render.Value(sum.Min, d.Unit)}, {"average", render.Value(sum.Avg, d.Unit)}, {"highest", render.Value(sum.Max, d.Unit)},
		} {
			x0 := mx + float64(i)*(tw+24)
			c.rrect(x0, 866, x0+tw, 966, 20, th.tile)
			labels = append(labels,
				label{x0 + tw/2, 916, st.val, 54, true, th.strong, 1},
				label{x0 + tw/2, 952, st.name + " " + string(d.Unit), 22, false, th.muted, 1},
			)
		}
	}

	out := image.NewRGBA(image.Rect(0, 0, px, px))
	draw.CatmullRom.Scale(out, out.Bounds(), c.img, c.img.Bounds(), draw.Src, nil)
	k := float64(px) / lg
	for _, l := range labels {
		f, err := face(l.bold, l.px*k*72.0/72.0)
		if err != nil {
			return nil, err
		}
		dr := &font.Drawer{Dst: out, Src: image.NewUniform(l.col), Face: f}
		x0 := l.x * k
		switch l.align {
		case 1:
			x0 -= float64(dr.MeasureString(l.s).Round()) / 2
		case 2:
			x0 -= float64(dr.MeasureString(l.s).Round())
		}
		dr.Dot = fixed.P(int(math.Round(x0)), int(math.Round(l.y*k)))
		dr.DrawString(l.s)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// yTicks picks the value gridlines: the target limits always, and round values
// in between when they do not crowd them.
func yTicks(lo, hi float64, r stats.Range, u render.Unit) []float64 {
	ticks := []float64{r.Low, r.High}
	step := 50.0
	if hi-lo < 140 {
		step = 25
	}
	for v := math.Ceil(lo/step) * step; v <= hi; v += step {
		if math.Abs(v-r.Low) < step*0.55 || math.Abs(v-r.High) < step*0.55 {
			continue
		}
		ticks = append(ticks, v)
	}
	return ticks
}

// xTicks picks clock-aligned times, few enough to keep the labels apart.
func xTicks(t0, t1 time.Time, loc *time.Location, room float64) []time.Time {
	span := t1.Sub(t0)
	for _, step := range []time.Duration{5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour} {
		if float64(span/step) <= room {
			var out []time.Time
			t := t0.In(loc).Truncate(step)
			for ; !t.After(t1); t = t.Add(step) {
				if !t.Before(t0) {
					out = append(out, t)
				}
			}
			return out
		}
	}
	return []time.Time{t0, t1}
}

// when is the header time: the activity if it is known, else the readings' span.
func when(d PhotoData, t0, t1 time.Time) string {
	if !d.Start.IsZero() && d.End.After(d.Start) {
		t0, t1 = d.Start, d.End
	}
	return fmt.Sprintf("%s - %s", t0.In(d.Loc).Format("2 Jan 2006, 15:04"), t1.In(d.Loc).Format("15:04"))
}
