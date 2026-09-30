package web

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MrCodeEU/glucava/internal/render"
)

func activityInput() ActivityChartInput {
	g := []TrendPoint{{T: 1000, V: 100}, {T: 2000, V: 180}, {T: 3000, V: 60}}
	return ActivityChartInput{
		Glucose: g, Low: 70, High: 180, VeryLow: 54, VeryHigh: 250, Unit: render.MgDL,
		Start: 1500, End: 2500, From: 500, To: 3500,
	}
}

func TestActivityChartOptionGlucoseOnly(t *testing.T) {
	opt := ActivityChartOption(activityInput())
	series := opt["series"].([]map[string]any)
	if len(series) != 1 {
		t.Fatalf("series = %d, want 1", len(series))
	}
	if n := len(opt["yAxis"].([]map[string]any)); n != 1 {
		t.Errorf("yAxis = %d, want 1 without heart rate or elevation", n)
	}
	b, err := json.Marshal(opt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Activity"`, `"Before"`, `"After"`, `"markLine"`, `"gv:mgdl"`, `"type":"piecewise"`, `"dataZoom"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("option is missing %s", want)
		}
	}
	// The window is fixed to the requested span, not squeezed to the readings.
	x := opt["xAxis"].(map[string]any)
	if x["min"].(int64) != 500 || x["max"].(int64) != 3500 {
		t.Errorf("xAxis window = %v..%v", x["min"], x["max"])
	}
}

func TestActivityChartOptionAllSeries(t *testing.T) {
	in := activityInput()
	in.HR = []TrendPoint{{T: 1500, V: 140.4}, {T: 2000, V: 160}}
	in.Elevation = []TrendPoint{{T: 1500, V: 300}, {T: 2000, V: 340}}
	in.Unit = render.MmolL
	opt := ActivityChartOption(in)
	series := opt["series"].([]map[string]any)
	if len(series) != 3 {
		t.Fatalf("series = %d, want 3", len(series))
	}
	axes := opt["yAxis"].([]map[string]any)
	if len(axes) != 3 {
		t.Fatalf("yAxis = %d, want glucose, heart rate and hidden elevation", len(axes))
	}
	if series[1]["yAxisIndex"] != 1 || series[2]["yAxisIndex"] != 2 {
		t.Errorf("axis indexes: hr=%v elevation=%v", series[1]["yAxisIndex"], series[2]["yAxisIndex"])
	}
	if axes[2]["show"] != false {
		t.Error("the elevation axis should be hidden")
	}
	b, _ := json.Marshal(opt)
	for _, want := range []string{`"gv:mmol"`, `"gv:bpm"`, `"gv:m"`, `Glucose (mmol/L)`, `"Heart rate (bpm)"`, `"Elevation (m)"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("option is missing %s", want)
		}
	}
	// 100 mg/dL is 5.55 mmol/L, rounded like every other chart.
	if !strings.Contains(string(b), "5.55") {
		t.Errorf("glucose not converted to mmol/L: %s", b)
	}
}

func TestActivityChartOptionDerivesWindowAndSkipsActivityShadeWithoutEnd(t *testing.T) {
	in := activityInput()
	in.From, in.To, in.Start, in.End = 0, 0, 0, 0
	opt := ActivityChartOption(in)
	x := opt["xAxis"].(map[string]any)
	if x["min"].(int64) != 1000 || x["max"].(int64) != 3000 {
		t.Errorf("derived window = %v..%v", x["min"], x["max"])
	}
	b, _ := json.Marshal(opt)
	if strings.Contains(string(b), `"Activity"`) {
		t.Error("no activity span should be shaded without a start and end")
	}
}
