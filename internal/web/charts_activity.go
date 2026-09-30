package web

import (
	"math"

	"github.com/MrCodeEU/glucava/internal/render"
)

// Colours of the extra activity-chart series. Heart rate is a pink that is
// not confused with the red/orange of the low bands.
const (
	colHR       = "#db2777"
	colElev     = "#94a3b8"
	colDuring   = "rgba(59,130,246,.12)"
	colBuffer   = "rgba(128,128,128,.07)"
	colLimitLow = "rgba(213,94,0,.85)"
)

// ActivityChartInput describes the interactive chart on the activity page:
// glucose against the target range, the activity and its before/after
// window shaded, heart rate on a second axis and altitude as a faint area.
// Times are Unix milliseconds, glucose is mg/dL. HR and Elevation may be
// empty; the chart then leaves out their axis and legend entry.
type ActivityChartInput struct {
	Glucose    []TrendPoint
	HR         []TrendPoint // V = bpm
	Elevation  []TrendPoint // V = metres
	Low, High  float64      // target range, mg/dL
	VeryLow    float64      // 0 leaves the line out
	VeryHigh   float64
	Unit       render.Unit
	Start, End int64 // the activity
	From, To   int64 // the whole window drawn (before ... after); 0 derives it from Glucose
}

// ActivityChartOption builds the multi-series option of the activity page.
func ActivityChartOption(in ActivityChartInput) map[string]any {
	u := in.Unit
	gf := glucoseFmt(u)
	from, to := in.From, in.To
	if len(in.Glucose) > 0 {
		if from == 0 || in.Glucose[0].T < from {
			from = in.Glucose[0].T
		}
		if to == 0 || in.Glucose[len(in.Glucose)-1].T > to {
			to = in.Glucose[len(in.Glucose)-1].T
		}
	}

	glucose := make([][]any, 0, len(in.Glucose))
	top := 0.0
	for _, p := range in.Glucose {
		v := conv(p.V, u)
		top = math.Max(top, v)
		glucose = append(glucose, []any{p.T, v})
	}
	lowV, highV := conv(in.Low, u), conv(in.High, u)

	areas := [][]map[string]any{
		{{"yAxis": lowV, "itemStyle": map[string]any{"color": colTarget}}, {"yAxis": highV}},
	}
	if in.End > in.Start {
		if in.Start > from {
			areas = append(areas, []map[string]any{
				{"name": "Before", "xAxis": from, "itemStyle": map[string]any{"color": colBuffer}, "label": map[string]any{"show": false}}, {"xAxis": in.Start}})
		}
		areas = append(areas, []map[string]any{
			{"name": "Activity", "xAxis": in.Start, "itemStyle": map[string]any{"color": colDuring},
				"label": map[string]any{"show": true, "position": "insideTop"}}, {"xAxis": in.End}})
		if in.End < to {
			areas = append(areas, []map[string]any{
				{"name": "After", "xAxis": in.End, "itemStyle": map[string]any{"color": colBuffer}, "label": map[string]any{"show": false}}, {"xAxis": to}})
		}
	}
	var lines []map[string]any
	limit := func(v float64, name string, col string) {
		if v <= 0 {
			return
		}
		lines = append(lines, map[string]any{
			"yAxis": conv(v, u), "name": name,
			"lineStyle": map[string]any{"color": col, "type": "dashed", "width": 1},
			"label":     map[string]any{"show": false},
		})
	}
	limit(in.VeryLow, "Very low", colLimitLow)
	limit(in.VeryHigh, "Very high", ColVeryHigh)

	glucoseSeries := map[string]any{
		"name": "Glucose (" + string(u) + ")", "type": "line", "data": glucose, "symbol": "none", "smooth": 0.15,
		"lineStyle": map[string]any{"width": 2.5}, "z": 5,
		"markArea": map[string]any{"silent": true, "data": areas},
		"tooltip":  map[string]any{"valueFormatter": gf},
	}
	if len(lines) > 0 {
		glucoseSeries["markLine"] = map[string]any{"silent": true, "symbol": "none", "data": lines}
	}
	series := []map[string]any{glucoseSeries}
	yAxes := []map[string]any{glucoseAxis(u, highV, highV, top)}
	legend := []string{glucoseSeries["name"].(string)}
	rightPad := 12

	if len(in.HR) > 0 {
		hr := make([][]any, 0, len(in.HR))
		for _, p := range in.HR {
			hr = append(hr, []any{p.T, math.Round(p.V)})
		}
		yAxes = append(yAxes, map[string]any{
			"type": "value", "scale": true, "position": "right", "splitLine": map[string]any{"show": false},
			"axisLabel": map[string]any{"formatter": fmtInt},
		})
		series = append(series, map[string]any{
			"name": "Heart rate (bpm)", "type": "line", "data": hr, "symbol": "none", "smooth": 0.2, "yAxisIndex": len(yAxes) - 1,
			"lineStyle": map[string]any{"width": 1.5, "color": colHR}, "itemStyle": map[string]any{"color": colHR},
			"tooltip": map[string]any{"valueFormatter": "gv:bpm"},
		})
		legend = append(legend, "Heart rate (bpm)")
		rightPad = 8
	}
	if len(in.Elevation) > 0 {
		lo, hi := math.Inf(1), math.Inf(-1)
		el := make([][]any, 0, len(in.Elevation))
		for _, p := range in.Elevation {
			lo, hi = math.Min(lo, p.V), math.Max(hi, p.V)
			el = append(el, []any{p.T, math.Round(p.V*10) / 10})
		}
		// A hidden axis stretched so the terrain fills only the lower part of
		// the plot and never fights the glucose curve for attention.
		span := math.Max(hi-lo, 10)
		yAxes = append(yAxes, map[string]any{
			"type": "value", "show": false, "min": math.Floor(lo - span*0.05), "max": math.Ceil(lo + span*3),
			"splitLine": map[string]any{"show": false},
		})
		series = append(series, map[string]any{
			"name": "Elevation (m)", "type": "line", "data": el, "symbol": "none", "yAxisIndex": len(yAxes) - 1, "z": 1,
			"lineStyle": map[string]any{"width": 0}, "itemStyle": map[string]any{"color": colElev},
			"areaStyle": map[string]any{"color": colElev, "opacity": 0.22},
			"tooltip":   map[string]any{"valueFormatter": "gv:m"},
		})
		legend = append(legend, "Elevation (m)")
	}

	return map[string]any{
		"grid":    grid(56, rightPad, 62, 8),
		"legend":  map[string]any{"top": 0, "right": 0, "icon": "roundRect", "itemHeight": 8, "data": legend},
		"tooltip": map[string]any{"trigger": "axis"},
		"xAxis":   map[string]any{"type": "time", "min": from, "max": to, "splitLine": map[string]any{"show": false}},
		"yAxis":   yAxes,
		"visualMap": map[string]any{
			"show": false, "type": "piecewise", "seriesIndex": 0, "dimension": 1,
			"pieces": []map[string]any{
				{"lt": lowV, "color": ColLow},
				{"gte": lowV, "lte": highV, "color": colLine},
				{"gt": highV, "color": ColHigh},
			},
		},
		"dataZoom": []map[string]any{
			{"type": "inside", "filterMode": "none"},
			{"type": "slider", "height": 22, "bottom": 8, "filterMode": "none"},
		},
		"series": series,
	}
}
