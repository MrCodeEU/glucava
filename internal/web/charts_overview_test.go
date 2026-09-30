package web

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPctMatrixOption(t *testing.T) {
	t.Parallel()
	opt := PctMatrixOption(PctMatrixInput{
		XLabels: []string{"a", "b"}, YLabels: []string{"Mon", "Tue"}, XEvery: 4,
		Cells: []PctCell{{0, 0, 92.345, "Mon a"}, {1, 1, 40, "Tue b"}, {5, 0, 1, "outside"}, {0, 9, 1, "outside"}},
	})
	b, err := json.Marshal(opt)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "outside") {
		t.Error("cells outside the grid were kept")
	}
	for _, want := range []string{`"gv:cell:pct"`, `"interval":3`, `"type":"continuous"`, `92.35`, `"inverse":true`} {
		if !strings.Contains(s, want) {
			t.Errorf("option is missing %s: %s", want, s)
		}
	}
}

func TestTrendThinSeries(t *testing.T) {
	t.Parallel()
	opt := TrendOption(TrendInput{
		Plain: true, Format: fmtPct,
		Series: []TrendSeries{{Name: "a", Points: []TrendPoint{{1, 1}}}, {Name: "b", Thin: true, Color: "#123456", Points: []TrendPoint{{1, 2}}}},
	})
	series := opt["series"].([]map[string]any)
	if w := series[0]["lineStyle"].(map[string]any)["width"]; w != 2 {
		t.Errorf("normal width = %v", w)
	}
	ls := series[1]["lineStyle"].(map[string]any)
	if ls["width"] != 1 || ls["opacity"] != 0.7 || ls["color"] != "#123456" {
		t.Errorf("thin line style = %v", ls)
	}
}
