// Package chartimg draws the small PNG charts embedded in emails. It uses
// only the standard library and x/image (no browser), so a mail never depends
// on Chrome being free. Shapes are drawn at 3x and scaled down for smooth
// edges; the labels are drawn afterwards at final size.
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
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

const (
	W, H  = 600, 220 // glucose chart size in pixels (the bar chart is as tall as its rows need)
	scale = 3
)

var (
	colBG    = color.RGBA{255, 255, 255, 255}
	colBand  = color.RGBA{220, 240, 226, 255}
	colSpan  = color.RGBA{225, 235, 250, 255}
	colGrid  = color.RGBA{225, 228, 232, 255}
	colLine  = color.RGBA{21, 101, 192, 255}
	colLow   = color.RGBA{198, 40, 40, 255}
	colHigh  = color.RGBA{230, 130, 0, 255}
	colInBar = color.RGBA{46, 160, 90, 255}
	colText  = color.RGBA{90, 98, 110, 255}
)

// palette holds the colours that change with the theme.
type palette struct{ bg, band, span, grid, line, text color.RGBA }

var (
	lightPalette = palette{colBG, colBand, colSpan, colGrid, colLine, colText}
	darkPalette  = palette{
		bg: color.RGBA{22, 27, 34, 255}, band: color.RGBA{28, 58, 42, 255}, span: color.RGBA{34, 48, 72, 255},
		grid: color.RGBA{56, 63, 73, 255}, line: color.RGBA{110, 168, 240, 255}, text: color.RGBA{160, 168, 180, 255},
	}
)

// Style is how the glucose chart looks. The zero value is the default look
// used in emails, so every switch is phrased as a change from it.
type Style struct {
	Dark         bool    // dark background
	Large        bool    // twice the pixels, for a sharper photo
	HideBand     bool    // no shaded target range
	HideActivity bool    // no shaded activity span
	HideDots     bool    // no red/orange dots on out-of-range readings
	HideHR       bool    // no heart rate curve, even when heart rate data is given
	LineWidth    float64 // curve thickness in logical pixels; 0 means 2
}

func (st Style) pal() palette {
	if st.Dark {
		return darkPalette
	}
	return lightPalette
}

func (st Style) factor() int {
	if st.Large {
		return 2
	}
	return 1
}

// Series is what Glucose draws. Values are mg/dL.
type Series struct {
	Samples    []stats.Sample
	Range      stats.Range
	Start, End time.Time // the activity, shaded
	Unit       render.Unit
	Loc        *time.Location
	Style      Style
}

type canvas struct {
	img *image.RGBA
	h   int // logical height
	k   int // output pixels per logical pixel
	s   float64
}

func newCanvas(h int, bg color.RGBA) *canvas { return newCanvasK(h, bg, 1) }

func newCanvasK(h int, bg color.RGBA, k int) *canvas {
	s := scale * k
	c := &canvas{image.NewRGBA(image.Rect(0, 0, W*s, h*s)), h, k, float64(s)}
	c.rect(0, 0, W, float64(h), bg)
	return c
}

// rect fills a rectangle given in final-size coordinates.
func (c *canvas) rect(x0, y0, x1, y1 float64, col color.RGBA) {
	r := image.Rect(int(math.Round(x0*c.s)), int(math.Round(y0*c.s)), int(math.Round(x1*c.s)), int(math.Round(y1*c.s))).Intersect(c.img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c.img.SetRGBA(x, y, col)
		}
	}
}

func (c *canvas) disc(cx, cy, r float64, col color.RGBA) {
	cx, cy, r = cx*c.s, cy*c.s, r*c.s
	for y := int(cy - r); y <= int(cy+r); y++ {
		for x := int(cx - r); x <= int(cx+r); x++ {
			if (float64(x)-cx)*(float64(x)-cx)+(float64(y)-cy)*(float64(y)-cy) <= r*r && image.Pt(x, y).In(c.img.Bounds()) {
				c.img.SetRGBA(x, y, col)
			}
		}
	}
}

func (c *canvas) line(x0, y0, x1, y1, w float64, col color.RGBA) {
	steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))*c.s) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		c.disc(x0+(x1-x0)*t, y0+(y1-y0)*t, w/2, col)
	}
}

// finish scales the canvas down, lets labels add text at final size, and encodes a PNG.
func (c *canvas) finish(labels func(*image.RGBA)) ([]byte, error) {
	out := image.NewRGBA(image.Rect(0, 0, W*c.k, c.h*c.k))
	draw.CatmullRom.Scale(out, out.Bounds(), c.img, c.img.Bounds(), draw.Src, nil)
	if labels != nil {
		// Labels use a fixed-size bitmap font: draw them at logical size, then
		// enlarge whole pixels so they stay crisp on the large chart.
		lab := image.NewRGBA(image.Rect(0, 0, W, c.h))
		labels(lab)
		draw.NearestNeighbor.Scale(out, out.Bounds(), lab, lab.Bounds(), draw.Over, nil)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func text(img *image.RGBA, x, y int, s string, right bool, col color.RGBA) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(col), Face: basicfont.Face7x13}
	if right {
		x -= d.MeasureString(s).Round()
	}
	d.Dot = fixed.P(x, y)
	d.DrawString(s)
}

// Glucose draws a glucose curve with the target band and the activity span.
func Glucose(s Series) ([]byte, error) {
	if len(s.Samples) == 0 {
		return nil, errors.New("chartimg: no samples")
	}
	if s.Loc == nil {
		s.Loc = time.Local
	}
	pts := append([]stats.Sample(nil), s.Samples...)
	sort.Slice(pts, func(i, j int) bool { return pts[i].Time.Before(pts[j].Time) })

	t0, t1 := pts[0].Time, pts[len(pts)-1].Time
	if !s.Start.IsZero() && s.Start.Before(t0) {
		t0 = s.Start
	}
	if s.End.After(t1) {
		t1 = s.End
	}
	if !t1.After(t0) {
		t1 = t0.Add(time.Minute)
	}
	lo, hi := math.Min(40, s.Range.Low-10), math.Max(250, s.Range.High+30)
	for _, p := range pts {
		lo, hi = math.Min(lo, p.Value-10), math.Max(hi, p.Value+10)
	}

	const padL, padR, padT, padB = 44.0, 12.0, 10.0, 26.0
	x := func(t time.Time) float64 { return padL + (W-padL-padR)*float64(t.Sub(t0))/float64(t1.Sub(t0)) }
	y := func(v float64) float64 { return padT + (H-padT-padB)*(1-(v-lo)/(hi-lo)) }

	pal := s.Style.pal()
	lw := s.Style.LineWidth
	if lw <= 0 {
		lw = 2
	}
	c := newCanvasK(H, pal.bg, s.Style.factor())
	if !s.Start.IsZero() && s.End.After(s.Start) && !s.Style.HideActivity {
		c.rect(x(s.Start), padT, x(s.End), H-padB, pal.span)
	}
	if !s.Style.HideBand {
		c.rect(padL, y(s.Range.High), W-padR, y(s.Range.Low), pal.band)
	}
	for _, v := range []float64{s.Range.Low, s.Range.High} {
		c.rect(padL, y(v)-0.5, W-padR, y(v)+0.5, pal.grid)
	}
	c.rect(padL, H-padB, W-padR, H-padB+1, pal.grid)
	for i := 1; i < len(pts); i++ {
		c.line(x(pts[i-1].Time), y(pts[i-1].Value), x(pts[i].Time), y(pts[i].Value), lw, pal.line)
	}
	for _, p := range pts {
		if s.Style.HideDots {
			break
		}
		switch {
		case p.Value < s.Range.Low:
			c.disc(x(p.Time), y(p.Value), 3, colLow)
		case p.Value > s.Range.High:
			c.disc(x(p.Time), y(p.Value), 3, colHigh)
		}
	}

	return c.finish(func(img *image.RGBA) {
		for _, v := range []float64{s.Range.Low, s.Range.High} {
			text(img, int(padL)-6, int(y(v))+4, render.Value(v, s.Unit), true, pal.text)
		}
		text(img, int(padL), H-8, t0.In(s.Loc).Format("15:04"), false, pal.text)
		text(img, W-int(padR), H-8, t1.In(s.Loc).Format("15:04"), true, pal.text)
		if mid := t0.Add(t1.Sub(t0) / 2); t1.Sub(t0) > 20*time.Minute {
			text(img, int(x(mid))-17, H-8, mid.In(s.Loc).Format("15:04"), false, pal.text)
		}
		text(img, W-int(padR), int(padT)+10, string(s.Unit), true, pal.text)
	})
}

// Bar is one row of the weekly chart: how an activity's readings split.
type Bar struct {
	Label                 string
	Below, InRange, Above float64 // percent; should add up to about 100
}

// Bars draws one stacked horizontal bar per activity: below, in and above range.
func Bars(bars []Bar) ([]byte, error) {
	if len(bars) == 0 {
		return nil, errors.New("chartimg: no bars")
	}
	if len(bars) > 12 {
		bars = bars[len(bars)-12:]
	}
	const padL, padR, padT = 176.0, 44.0, 10.0
	const rowH = 26.0
	c := newCanvas(int(padT+rowH*float64(len(bars)))+10, colBG)
	for i, b := range bars {
		top := padT + float64(i)*rowH
		x := padL
		for _, seg := range []struct {
			v   float64
			col color.RGBA
		}{{b.Below, colLow}, {b.InRange, colInBar}, {b.Above, colHigh}} {
			w := (W - padL - padR) * math.Max(0, seg.v) / 100
			c.rect(x, top+3, x+w, top+rowH-3, seg.col)
			x += w
		}
	}
	return c.finish(func(img *image.RGBA) {
		for i, b := range bars {
			top := padT + float64(i)*rowH
			label := fit(b.Label, 24)
			text(img, int(padL)-8, int(top+rowH/2)+4, label, true, colText)
			text(img, int(W-padR)+6, int(top+rowH/2)+4, fmt.Sprintf("%.0f%%", b.InRange), false, colText)
		}
	})
}

// fit makes s drawable with the built-in bitmap font: it has only ASCII
// glyphs, so umlauts are transliterated and other characters become "?".
// It cuts s to at most n characters, ending in "..." when it had to cut.
func fit(s string, n int) string {
	var out []rune
	for _, r := range s {
		switch {
		case r >= 0x20 && r < 0x7f:
			out = append(out, r)
		default:
			t, ok := translit[r]
			if !ok {
				t = "?"
			}
			out = append(out, []rune(t)...)
		}
	}
	if len(out) > n {
		out = append(out[:n-3], '.', '.', '.')
	}
	return string(out)
}

var translit = map[rune]string{
	'ä': "a", 'ö': "o", 'ü': "u", 'Ä': "A", 'Ö': "O", 'Ü': "U", 'ß': "ss",
	'é': "e", 'è': "e", 'ê': "e", 'á': "a", 'à': "a", 'â': "a", 'ó': "o", 'ô': "o", 'í': "i", 'ú': "u", 'ç': "c", 'ñ': "n",
	'–': "-", '—': "-", '·': "-", '\u2019': "'",
}
