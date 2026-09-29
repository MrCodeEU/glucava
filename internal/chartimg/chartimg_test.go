package chartimg

import (
	"bytes"
	"image/color"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

func series() Series {
	start := time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC)
	var s []stats.Sample
	for i := 0; i < 70; i++ {
		v := 120 + 70*math.Sin(float64(i)/9) - float64(i)/2
		s = append(s, stats.Sample{Time: start.Add(time.Duration(i) * 5 * time.Minute), Value: v})
	}
	return Series{Samples: s, Range: stats.DefaultRange, Start: start.Add(60 * time.Minute), End: start.Add(150 * time.Minute), Unit: render.MgDL, Loc: time.UTC}
}

func decode(t *testing.T, b []byte) (w, h int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	return img.Bounds().Dx(), img.Bounds().Dy()
}

func TestGlucoseChartIsAPNGOfFixedSize(t *testing.T) {
	b, err := Glucose(series())
	if err != nil {
		t.Fatal(err)
	}
	if w, h := decode(t, b); w != W || h != H {
		t.Errorf("size = %dx%d", w, h)
	}
	if len(b) > 100_000 {
		t.Errorf("PNG is %d bytes; keep mail images small", len(b))
	}
	if p := os.Getenv("CHARTIMG_DUMP"); p != "" {
		_ = os.WriteFile(p+"/glucose.png", b, 0o644)
	}
}

func TestGlucoseMmolAndSinglePoint(t *testing.T) {
	s := series()
	s.Unit = render.MmolL
	if _, err := Glucose(s); err != nil {
		t.Fatal(err)
	}
	one := Series{Samples: s.Samples[:1], Range: stats.DefaultRange}
	if _, err := Glucose(one); err != nil {
		t.Fatalf("a single reading must not crash: %v", err)
	}
	if _, err := Glucose(Series{}); err == nil {
		t.Error("no samples must be an error")
	}
}

// TestStyleHasAndPanels covers the has()/panels() logic behind Panels:
// the zero value means every panel (today's default look, unchanged),
// a non-nil empty slice means none, and a specific list means only those.
func TestStyleHasAndPanels(t *testing.T) {
	var zero Style
	for _, p := range []string{PanelActivity, PanelBand, PanelDots, PanelHR} {
		if !zero.has(p) {
			t.Errorf("zero-value Style should have panel %q", p)
		}
	}
	none := Style{Panels: []string{}}
	for _, p := range []string{PanelActivity, PanelBand, PanelDots, PanelHR} {
		if none.has(p) {
			t.Errorf("Style{Panels: []string{}} should have no panel %q", p)
		}
	}
	bandOnly := Style{Panels: []string{PanelBand}}
	if !bandOnly.has(PanelBand) || bandOnly.has(PanelActivity) || bandOnly.has(PanelDots) || bandOnly.has(PanelHR) {
		t.Errorf("Style{Panels: [band]} should have only band")
	}
}

// TestPanelOrderAffectsOverlapRendering is the point of Panels being an
// order, not just a set: activity and band are both full-height/full-width
// shaded rects that can overlap, and whichever is listed later must win
// there. This proves reordering them actually changes the rendered chart.
func TestPanelOrderAffectsOverlapRendering(t *testing.T) {
	activityFirst := series()
	activityFirst.Style = Style{Panels: []string{PanelActivity, PanelBand}}
	bandFirst := series()
	bandFirst.Style = Style{Panels: []string{PanelBand, PanelActivity}}

	imgA, err := Glucose(activityFirst)
	if err != nil {
		t.Fatal(err)
	}
	imgB, err := Glucose(bandFirst)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(imgA, imgB) {
		t.Error("reordering activity/band panels should change the rendered chart where they overlap")
	}
}

func TestBars(t *testing.T) {
	bars := []Bar{{"Easy run", 0, 92, 8}, {"Intervals", 6, 71, 23}, {"Long run with a very long name here", 0, 85, 15}}
	b, err := Bars(bars)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := decode(t, b); w != W || h >= H {
		t.Errorf("3 bars should be shorter than the glucose chart: %dx%d", w, h)
	}
	if p := os.Getenv("CHARTIMG_DUMP"); p != "" {
		_ = os.WriteFile(p+"/bars.png", b, 0o644)
	}
	many := make([]Bar, 30)
	if _, err := Bars(many); err != nil {
		t.Fatalf("many bars: %v", err)
	}
	if _, err := Bars(nil); err == nil {
		t.Error("no bars must be an error")
	}
}

func TestFitTransliteratesAndCutsByCharacter(t *testing.T) {
	for in, want := range map[string]string{
		"Easy Run":                       "Easy Run",
		"Läufer Übung ß":                 "Laufer Ubung ss",
		"Lauf 🏃 heute":                   "Lauf ? heute",
		"a very long activity name here": "a very long activity ...",
		"Ünïcödé":                        "Un?code",
	} {
		if got := fit(in, 24); got != want {
			t.Errorf("fit(%q) = %q, want %q", in, got, want)
		}
	}
	if got := fit("ääääääääääääääääääääääääää", 24); len([]rune(got)) != 24 || !strings.HasSuffix(got, "...") {
		t.Errorf("multi-byte cut: %q", got)
	}
}

func photoData() PhotoData {
	start := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	var ss []stats.Sample
	var hr []HRPoint
	for i := 0; i < 24; i++ {
		ss = append(ss, stats.Sample{Time: start.Add(time.Duration(i) * 5 * time.Minute), Value: 110 + float64(i%7)*15})
		hr = append(hr, HRPoint{Time: start.Add(time.Duration(i) * 5 * time.Minute), BPM: 120 + float64(i)})
	}
	return PhotoData{Samples: ss, HR: hr, Range: stats.DefaultRange, Start: start, End: start.Add(100 * time.Minute), Unit: render.MgDL, Loc: time.UTC}
}

func TestPhotoIsSquareAndSizesFollowStyle(t *testing.T) {
	for _, tc := range []struct {
		style Style
		side  int
	}{{Style{}, 1080}, {Style{Large: true}, 1620}, {Style{Dark: true, Panels: []string{}, LineWidth: 4}, 1080}} {
		d := photoData()
		d.Style = tc.style
		b, err := Photo(d)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil || cfg.Width != tc.side || cfg.Height != tc.side {
			t.Errorf("%+v: %dx%d %v, want %d square", tc.style, cfg.Width, cfg.Height, err, tc.side)
		}
	}
}

func TestPhotoNeedsSamplesAndToleratesOddData(t *testing.T) {
	if _, err := Photo(PhotoData{}); err == nil {
		t.Error("no samples must be an error")
	}
	d := photoData()
	d.Samples = d.Samples[:1] // a single reading
	d.HR = nil
	d.Start, d.End = time.Time{}, time.Time{}
	if _, err := Photo(d); err != nil {
		t.Errorf("single reading: %v", err)
	}
	d = photoData()
	d.Unit = render.MmolL
	d.HR = d.HR[:1]
	if _, err := Photo(d); err != nil {
		t.Errorf("mmol/L: %v", err)
	}
}

// TestPhotoOverlayTogglesChangeOutput guards each new 0.3.0 overlay: on its
// own, it must render successfully and actually change the pixels versus the
// baseline (otherwise the toggle would silently do nothing, the same class
// of bug as the panel reorder buttons in 0.2.0).
func TestPhotoOverlayTogglesChangeOutput(t *testing.T) {
	base := photoData()
	basePNG, err := Photo(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		style func(*Style)
	}{
		{"avg line", func(s *Style) { s.AvgLine = true }},
		{"range lines", func(s *Style) { s.RangeLines = true }},
		{"min/max markers", func(s *Style) { s.MinMax = true }},
		{"hide stats", func(s *Style) { s.HideStats = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := photoData()
			tc.style(&d.Style)
			b, err := Photo(d)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(b, basePNG) {
				t.Error("output identical to the baseline: the toggle changed nothing")
			}
		})
	}
}

// TestPhotoHideStatsActuallyHidesTheBar checks HideStats by pixel content,
// not just "the bytes differ": the stats bar sits in a known band near the
// bottom (y 748-996 of the 1000-unit layout), which should be untouched
// background when hidden.
func TestPhotoHideStatsActuallyHidesTheBar(t *testing.T) {
	d := photoData()
	d.Style.HideStats = true
	b, err := Photo(d)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	bg := lightPalette.bg
	// A horizontal strip through where the "in range" bar (green) would be.
	y := bounds.Min.Y + int(float64(bounds.Dy())*0.76)
	for x := bounds.Min.X + bounds.Dx()/2 - 20; x < bounds.Min.X+bounds.Dx()/2+20; x++ {
		r, g, bch, _ := img.At(x, y).RGBA()
		got := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bch >> 8), 255}
		if got != bg {
			t.Fatalf("pixel at (%d,%d) = %+v, want background %+v: the stats bar is still drawn", x, y, got, bg)
		}
	}
}
