package web

import (
	"encoding/json"
	stdhtml "html"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/render"
)

// formatterKinds are the "gv:" names static/charts.js implements.
var formatterKinds = map[string]bool{
	"mgdl": true, "mmol": true, "pct": true, "int": true, "num": true, "hhmm": true, "min": true,
	"rawaxis": true, "point": true, "cell": true,
}

// walk visits every value in a decoded JSON tree.
func walk(v any, f func(key string, v any)) {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			f(k, c)
			walk(c, f)
		}
	case []any:
		for _, c := range t {
			walk(c, f)
		}
	}
}

// roundTrip marshals opt and decodes it back generically.
func roundTrip(t *testing.T, opt any) map[string]any {
	t.Helper()
	b, err := json.Marshal(opt)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func demoOptions(u render.Unit) map[string]map[string]any {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	return map[string]map[string]any{
		"donut": DonutOption(DonutInput{Slices: []DonutSlice{{"Low", 5, ColLow}, {"In", 0, ColInRange}, {"High", 95, ColHigh}}, Centre: "95%", Legend: true}),
		"agp": AGPOption(AGPInput{Low: 70, High: 180, Unit: u, Points: []AGPPoint{
			{0, 60, 90, 110, 140, 190}, {720, 70, 100, 120, 150, 210}}}),
		"heat": HeatmapOption(HeatmapInput{Days: []string{"Mon", "Tue"}, Low: 70, High: 180, Unit: u, Cells: []HeatCell{
			{0, 3, 100}, {1, 23, 200}, {5, 1, 1}, {0, 24, 1}}}),
		"trend": TrendOption(TrendInput{Zoom: true, Low: 70, High: 180, Unit: u, Series: []TrendSeries{
			{Name: "avg", Points: []TrendPoint{{now.UnixMilli(), 120}, {now.Add(time.Hour).UnixMilli(), 130}}}}}),
		"plain": TrendOption(TrendInput{Plain: true, Format: fmtPct, Series: []TrendSeries{{Name: "tir", Points: []TrendPoint{{now.UnixMilli(), 71}}}}}),
		"bar":   BarOption(BarInput{Categories: []string{"a", "b"}, Horizontal: true, Legend: true, Series: []BarSeries{{Name: "x", Stack: "s", Values: []float64{1, 2}}}}),
		"scatter": ScatterOption(ScatterInput{XFormat: fmtMin, YFormat: fmtMgdl,
			Points: []ScatterPoint{{X: 30, Y: 120, Label: "run"}}}),
	}
}

func TestBuildersProduceSerialisableOptions(t *testing.T) {
	for _, u := range []render.Unit{render.MgDL, render.MmolL} {
		for name, opt := range demoOptions(u) {
			m := roundTrip(t, opt)
			series, _ := m["series"].([]any)
			if len(series) == 0 {
				t.Errorf("%s/%s: no series", name, u)
			}
			walk(m, func(k string, v any) {
				s, ok := v.(string)
				if !ok || !strings.HasPrefix(s, "gv:") {
					return
				}
				if k != "formatter" && k != "valueFormatter" {
					t.Errorf("%s/%s: %q under %q, only formatter keys are revived", name, u, s, k)
				}
				if kind := strings.Split(s[3:], ":")[0]; !formatterKinds[kind] {
					t.Errorf("%s/%s: unknown formatter %q", name, u, s)
				}
			})
		}
	}
}

func TestDonutDropsEmptySlices(t *testing.T) {
	m := roundTrip(t, demoOptions(render.MgDL)["donut"])
	data := m["series"].([]any)[0].(map[string]any)["data"].([]any)
	if len(data) != 2 {
		t.Errorf("donut slices = %d, want 2 (zero slice dropped)", len(data))
	}
}

func TestAGPStackSumsToPercentiles(t *testing.T) {
	m := roundTrip(t, demoOptions(render.MgDL)["agp"])
	series := m["series"].([]any)
	// base + three bands: the running sum of stacked values at minute 0 must
	// reach each real percentile (60, 90, 140, 190).
	want := []float64{60, 90, 140, 190}
	sum := 0.0
	for i := 0; i < 4; i++ {
		item := series[i].(map[string]any)["data"].([]any)[0].([]any)
		sum += item[1].(float64)
		if sum != want[i] {
			t.Errorf("stack level %d = %v, want %v", i, sum, want[i])
		}
		if i > 0 && item[2].(float64) != want[i] {
			t.Errorf("band %d real value = %v, want %v", i, item[2], want[i])
		}
	}
	if series[len(series)-1].(map[string]any)["name"] != "Median" {
		t.Error("median must be the last series")
	}
}

func TestUnitConversion(t *testing.T) {
	if got := conv(180.16, render.MmolL); got != 10 {
		t.Errorf("conv mmol = %v, want 10", got)
	}
	if got := conv(120.04, render.MgDL); got != 120 {
		t.Errorf("conv mg/dL = %v, want 120", got)
	}
	m := roundTrip(t, demoOptions(render.MmolL)["agp"])
	y := m["yAxis"].(map[string]any)
	if y["min"].(float64) != 2 || y["max"].(float64) > 20 {
		t.Errorf("mmol axis = %v..%v, want 2..<=20", y["min"], y["max"])
	}
}

func TestHeatmapSkipsOutOfRangeCells(t *testing.T) {
	m := roundTrip(t, demoOptions(render.MgDL)["heat"])
	data := m["series"].([]any)[0].(map[string]any)["data"].([]any)
	if len(data) != 2 {
		t.Errorf("heatmap cells = %d, want 2 (bad day/hour dropped)", len(data))
	}
}

var optAttr = regexp.MustCompile(`data-option="([^"]*)"`)

func TestChartElement(t *testing.T) {
	opt := map[string]any{"title": map[string]any{"text": `a "quoted" <b>&'`}}
	var sb strings.Builder
	if err := Chart("c1", opt, 240).Render(&sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{"<gv-chart", `id="c1"`, `data-height="240"`, `role="img"`, "height:240px"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	mt := optAttr.FindStringSubmatch(out)
	if mt == nil {
		t.Fatalf("no data-option in %s", out)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(stdhtml.UnescapeString(mt[1])), &back); err != nil {
		t.Fatalf("data-option is not valid JSON after unescaping: %v", err)
	}
	if got := back["title"].(map[string]any)["text"]; got != `a "quoted" <b>&'` {
		t.Errorf("round trip = %q", got)
	}
	if strings.Contains(out, "<b>") {
		t.Error("attribute value must not contain a raw tag")
	}
}

func TestChartsDemoRoute(t *testing.T) {
	e := newEnv(t)
	c := e.login(t)
	if w := e.get(t, "/ui/charts", c); w.Code != http.StatusNotFound {
		t.Errorf("/ui/charts on a test build = %d, want 404", w.Code)
	}

	e.srv.Demo = true
	e.h = e.srv.Handler()
	if w := e.get(t, "/ui/charts", nil); w.Code != http.StatusSeeOther {
		t.Errorf("/ui/charts without login = %d, want redirect", w.Code)
	}
	w := e.get(t, "/ui/charts", c)
	if w.Code != http.StatusOK {
		t.Fatalf("/ui/charts = %d", w.Code)
	}
	for _, id := range []string{"demo-donut", "demo-agp", "demo-heat", "demo-trend", "demo-bar", "demo-scatter", "/static/echarts.min.js", "/static/charts.js"} {
		if !strings.Contains(w.Body.String(), id) {
			t.Errorf("demo page lacks %s", id)
		}
	}
	if w := e.get(t, "/static/echarts.min.js", nil); w.Code != http.StatusOK || w.Body.Len() < 100_000 {
		t.Errorf("echarts.min.js = %d, %d bytes", w.Code, w.Body.Len())
	}

	dev := newEnv(t)
	dev.srv.Build = "dev"
	dev.h = dev.srv.Handler()
	if w := dev.get(t, "/ui/charts", dev.login(t)); w.Code != http.StatusOK {
		t.Errorf("/ui/charts on a dev build = %d, want 200", w.Code)
	}
}
