package chartimg

import (
	"bytes"
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
	}{{Style{}, 1080}, {Style{Large: true}, 1620}, {Style{Dark: true, HideBand: true, HideActivity: true, HideDots: true, HideHR: true, LineWidth: 4}, 1080}} {
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
