package report

import (
	"bytes"
	"context"
	"encoding/xml"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

var vienna = mustLoc("Europe/Vienna")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return l
}

// fixture is 30 days of five-minute readings with a daily rhythm, a few
// lows and highs, and a handful of activities. Everything is deterministic.
func fixture(compare bool) Input {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, vienna)
	from := now.AddDate(0, 0, -30)
	var samples []stats.Sample
	for t := from; t.Before(now); t = t.Add(5 * time.Minute) {
		h := float64(t.In(vienna).Hour()) + float64(t.Minute())/60
		day := t.Sub(from).Hours() / 24
		v := 115 + 28*math.Sin((h-7)/24*2*math.Pi) + 12*math.Sin(day*1.7+h/3)
		if int(day)%9 == 4 && h > 2.5 && h < 3.5 { // a night low
			v = 52
		}
		if int(day)%11 == 6 && h > 13 && h < 14.5 { // a high after lunch
			v = 235
		}
		samples = append(samples, stats.Sample{Time: t, Value: v})
	}
	var acts []jobs.Activity
	for i, d := range []int{2, 5, 8, 13, 19, 26} {
		start := time.Date(2026, 9, 30-d, 7+i%3, 15, 0, 0, vienna)
		a := jobs.Activity{StravaID: string(rune('a' + i)), Name: "Morning Run", Sport: "Run", Start: start,
			Duration: 40 * time.Minute, Distance: 7200, ElevationGain: 60, Status: jobs.StatusDone}
		if sum, ok := stats.Summarize(within(samples, start, start.Add(a.Duration)), stats.Range{Low: 70, High: 180}); ok {
			a.Summary = &sum
		}
		acts = append(acts, a)
	}
	in := Input{
		From: from, To: now, Samples: samples, Acts: acts,
		HR:      map[string]HRStat{"a": {Avg: 152, Max: 171}},
		Sources: []Source{{Name: "dexcom", Count: int64(len(samples)), Latest: now.Add(-3 * time.Minute)}},
		Thr:     analytics.Thresholds{VeryLow: 54, Low: 70, High: 180, VeryHigh: 250},
		Unit:    render.MgDL, Loc: vienna, Now: now, Build: "test",
	}
	if compare {
		in.Compare, in.PrevTo, in.PrevFrom = true, from, from.AddDate(0, 0, -30)
		for t := in.PrevFrom; t.Before(in.PrevTo); t = t.Add(5 * time.Minute) {
			in.PrevSamples = append(in.PrevSamples, stats.Sample{Time: t, Value: 130 + 40*math.Sin(t.Sub(in.PrevFrom).Hours()/5)})
		}
	}
	return in
}

func within(s []stats.Sample, from, to time.Time) []stats.Sample {
	var out []stats.Sample
	for _, x := range s {
		if !x.Time.Before(from) && x.Time.Before(to) {
			out = append(out, x)
		}
	}
	return out
}

func TestModelAndTemplateData(t *testing.T) {
	m := Build(fixture(true))
	if !m.HasData || m.KPIs.Count == 0 {
		t.Fatalf("no data: %+v", m.KPIs)
	}
	if got := m.TIR.VeryLow + m.TIR.Low + m.TIR.InRange + m.TIR.High + m.TIR.VeryHigh; math.Abs(got-100) > 0.01 {
		t.Errorf("bands sum to %.2f", got)
	}
	if len(m.Acts) != 6 || len(m.Sports) != 1 || m.Sports[0].Sport != "Run" {
		t.Errorf("activities/sports: %d, %+v", len(m.Acts), m.Sports)
	}
	if m.EpStats[analytics.KindVeryLow].Count == 0 || m.EpStats[analytics.KindHigh].Count == 0 {
		t.Errorf("fixture lows/highs not detected: %+v", m.EpStats)
	}

	d := m.template()
	if d.Range != "31 Aug – 30 Sep 2026" {
		t.Errorf("range = %q", d.Range)
	}
	if len(d.Kpis) != 6 || len(d.Bands) != 5 || len(d.Parts) != 4 || len(d.Episodes) != 4 {
		t.Fatalf("shape: kpis=%d bands=%d parts=%d episodes=%d", len(d.Kpis), len(d.Bands), len(d.Parts), len(d.Episodes))
	}
	if d.Kpis[0].Delta == "" || !strings.HasSuffix(d.Kpis[0].Delta, "vs previous") {
		t.Errorf("compare delta missing: %+v", d.Kpis[0])
	}
	if d.Compare == "" {
		t.Error("compare line missing")
	}
	for _, k := range d.Kpis {
		if strings.Contains(k.Value, "NaN") || strings.Contains(k.Delta, "NaN") {
			t.Errorf("NaN in %+v", k)
		}
	}
	if len(d.Recent) == 0 || len(d.Recent) > maxRecentEpisodes {
		t.Errorf("recent = %d", len(d.Recent))
	}

	// mmol/L changes the numbers and the unit label, not the structure.
	in := fixture(false)
	in.Unit = render.MmolL
	dm := Build(in).template()
	if dm.Unit != "mmol/L" || dm.Kpis[1].Value == d.Kpis[1].Value {
		t.Errorf("mmol/L not applied: %+v vs %+v", dm.Kpis[1], d.Kpis[1])
	}
}

func TestEmptyRange(t *testing.T) {
	in := fixture(false)
	in.Samples, in.Acts = nil, nil
	m := Build(in)
	if m.HasData {
		t.Fatal("HasData with no samples")
	}
	files, err := m.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["agp.svg"]; ok {
		t.Error("charts written for an empty report")
	}
	if !bytes.Contains(files["data.json"], []byte(`"hasData": false`)) {
		t.Errorf("data.json = %s", files["data.json"])
	}
}

func TestAllTimeStartsAtFirstReading(t *testing.T) {
	in := fixture(false)
	in.AllTime, in.From = true, in.To.AddDate(-10, 0, 0)
	m := Build(in)
	want := time.Date(2026, 8, 31, 0, 0, 0, 0, vienna)
	if !m.From.Equal(want) {
		t.Errorf("From = %v, want %v", m.From, want)
	}
}

func TestSVGsAreWellFormed(t *testing.T) {
	m := Build(fixture(false))
	files, err := m.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tir.svg", "agp.svg", "trend.svg", "parts.svg"} {
		b := files[name]
		if len(b) == 0 {
			t.Fatalf("%s missing", name)
		}
		assertSVG(t, name, b)
	}
	// Degenerate inputs stay well-formed as well.
	assertSVG(t, "empty tir", TIRBarSVG(analytics.TIR5{}))
	assertSVG(t, "empty agp", AGPSVG(analytics.AGP{}, m.In.Thr, render.MgDL))
	assertSVG(t, "no days", TrendSVG(nil, m.In.Thr, render.MgDL, vienna))
	assertSVG(t, "one day", TrendSVG(m.Daily[:1], m.In.Thr, render.MmolL, vienna))
	assertSVG(t, "empty parts", DayPartsSVG([4]analytics.DayPart{}))
}

var badNumber = regexp.MustCompile(`NaN|Inf`)

func assertSVG(t *testing.T, name string, b []byte) {
	t.Helper()
	if badNumber.Match(b) {
		t.Errorf("%s contains NaN or Inf", name)
	}
	dec := xml.NewDecoder(bytes.NewReader(b))
	n := 0
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() != "EOF" {
				t.Errorf("%s is not well-formed XML: %v", name, err)
			}
			break
		}
		n++
	}
	if n < 3 || !bytes.HasPrefix(b, []byte("<svg ")) || !bytes.HasSuffix(b, []byte("</svg>")) {
		t.Errorf("%s: not an svg document", name)
	}
}

func TestTemplateTextStaysInsideTheFontSubset(t *testing.T) {
	for _, compare := range []bool{false, true} {
		in := fixture(compare)
		in.Acts[0].Name = "Läufer 🏃 – Süd → Ost · 10 km… 朝ラン"
		in.Sources[0].Name = "dexcom ✓"
		files, err := Build(in).Files()
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"data.json", "agp.svg", "trend.svg", "parts.svg"} {
			for _, r := range string(files[name]) {
				if r != '\n' && !inSubset(r) {
					t.Errorf("%s contains %q (U+%04X), which the embedded font subset lacks", name, r, r)
				}
			}
		}
	}
}

func TestSVGIsDeterministic(t *testing.T) {
	a, _ := Build(fixture(false)).Files()
	b, _ := Build(fixture(false)).Files()
	for name := range a {
		if !bytes.Equal(a[name], b[name]) {
			t.Errorf("%s differs between two builds", name)
		}
	}
}

func TestFindTypstOverride(t *testing.T) {
	t.Setenv(EnvTypst, filepath.Join(t.TempDir(), "nope"))
	if _, err := FindTypst(); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("err = %v", err)
	}
}

// typstOrSkip finds Typst or skips: the PDF tests need the real binary, which
// CI installs and which the Docker image ships.
func typstOrSkip(t *testing.T) string {
	t.Helper()
	p, err := FindTypst()
	if err != nil {
		t.Skip("typst not available: " + err.Error())
	}
	return p
}

var pageCount = regexp.MustCompile(`/Type\s*/Page[^s]`)

func TestPDFSmoke(t *testing.T) {
	typst := typstOrSkip(t)
	m := Build(fixture(true))
	start := time.Now()
	pdf, err := m.PDF(context.Background(), typst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", pdf[:min(len(pdf), 16)])
	}
	if len(pdf) < 20<<10 || len(pdf) > 2<<20 {
		t.Errorf("size %d bytes", len(pdf))
	}
	if n := len(pageCount.FindAll(pdf, -1)); n < 1 || n > 2 {
		t.Errorf("%d pages, want 1-2", n)
	}
	t.Logf("pdf %d bytes in %s", len(pdf), time.Since(start).Round(10*time.Millisecond))

	// Same input, same bytes: the fixed font and creation timestamp make the
	// output reproducible.
	again, err := m.PDF(context.Background(), typst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pdf, again) {
		t.Error("two renders of the same model differ")
	}
}

func TestPDFEmptyRange(t *testing.T) {
	typst := typstOrSkip(t)
	in := fixture(false)
	in.Samples, in.Acts = nil, nil
	pdf, err := Build(in).PDF(context.Background(), typst)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("empty report: %v", err)
	}
}

func TestPDFTimesOut(t *testing.T) {
	typstOrSkip(t)
	sleeper := filepath.Join(t.TempDir(), "typst")
	if err := os.WriteFile(sleeper, []byte("#!/bin/sh\nsleep 60\n"), 0o700); err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := Build(fixture(false)).PDF(ctx, sleeper); err == nil {
		t.Fatal("no error from a hung typst")
	}
}

// TestRenderPages writes PNGs of every page when REPORT_PNG_DIR is set, to
// look at the layout: REPORT_PNG_DIR=/tmp/pages go test ./internal/report -run Pages
func TestRenderPages(t *testing.T) {
	dir := os.Getenv("REPORT_PNG_DIR")
	if dir == "" {
		t.Skip("REPORT_PNG_DIR not set")
	}
	typst := typstOrSkip(t)
	for name, in := range map[string]Input{"mgdl": fixture(true), "mmol": func() Input { i := fixture(false); i.Unit = render.MmolL; return i }()} {
		pages, err := Build(in).PNGs(context.Background(), typst, 110)
		if err != nil {
			t.Fatal(err)
		}
		for i, p := range pages {
			if err := os.WriteFile(filepath.Join(dir, name+"-"+string(rune('1'+i))+".png"), p, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
