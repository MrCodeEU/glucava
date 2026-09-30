package web

import (
	"encoding/json"
	"fmt"
	"math"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/render"
)

// Chart renders an ECharts option as a <gv-chart> element (see
// static/charts.js). id must be stable and unique on the page: Datastar morphs
// by id, and the element keeps its rendered chart and only re-renders when
// data-option changes. The page must load /static/echarts.min.js and
// /static/charts.js. opt is anything that marshals to an ECharts option, such
// as the maps the builders below return.
func Chart(id string, opt any, height int) g.Node {
	b, err := json.Marshal(opt)
	if err != nil {
		b = []byte("{}")
	}
	return g.El("gv-chart",
		ID(id),
		g.Attr("role", "img"),
		g.Attr("data-option", string(b)),
		g.Attr("data-height", fmt.Sprint(height)),
		Style(fmt.Sprintf("display:block;height:%dpx", height)),
	)
}

// ChartScripts loads the chart library. Include it once per page that uses Chart.
func ChartScripts() g.Node {
	return g.Group{
		Script(Src("/static/echarts.min.js"), Defer()),
		Script(Src("/static/charts.js"), Defer()),
	}
}

// Formatter names understood by static/charts.js. A string under a
// "formatter" or "valueFormatter" key that starts with "gv:" selects one.
const (
	fmtMgdl = "gv:mgdl"
	fmtMmol = "gv:mmol"
	fmtPct  = "gv:pct"
	fmtInt  = "gv:int"
	fmtHHMM = "gv:hhmm" // minutes since midnight
	fmtMin  = "gv:min"  // a duration in minutes
)

// Fixed colours, chosen to stay distinguishable with red-green colour
// blindness and to read on both the light and dark surface. Theme colours
// (text, axes, tooltips) come from the CSS variables instead.
const (
	ColVeryLow  = "#b91c1c"
	ColLow      = "#d55e00"
	ColInRange  = "#2a9d6f"
	ColHigh     = "#e6b800"
	ColVeryHigh = "#a86b00"
	colLine     = "#3b82f6"
	colBand     = "rgba(59,130,246,.14)"
	colBandIn   = "rgba(59,130,246,.30)"
	colTarget   = "rgba(42,157,111,.10)"
)

// glucoseFmt names the formatter for a glucose value in unit u.
func glucoseFmt(u render.Unit) string {
	if u == render.MmolL {
		return fmtMmol
	}
	return fmtMgdl
}

// conv converts a stored mg/dL value to unit u, rounded for compact JSON.
func conv(v float64, u render.Unit) float64 {
	if u == render.MmolL {
		return math.Round(v/18.016*100) / 100
	}
	return math.Round(v*10) / 10
}

// glucoseAxis is a value axis in unit u, with room for the target range.
func glucoseAxis(u render.Unit, low, high, top float64) map[string]any {
	lo, step := 40.0, 40.0
	if u == render.MmolL {
		lo, step = 2.0, 2.0
	}
	hi := math.Max(top, high*1.15)
	hi = math.Ceil(hi/step) * step
	return map[string]any{
		"type": "value", "min": lo, "max": hi, "interval": step,
		"axisLabel": map[string]any{"formatter": glucoseFmt(u)},
	}
}

func grid(top, right, bottom, left int) map[string]any {
	return map[string]any{"top": top, "right": right, "bottom": bottom, "left": left, "containLabel": true}
}

// DonutSlice is one segment of a donut: Value is a percentage.
type DonutSlice struct {
	Label string
	Value float64
	Color string
}

// DonutInput describes a donut such as a time-in-range split. Centre and Sub
// are the big and small text in the hole.
type DonutInput struct {
	Slices      []DonutSlice
	Centre, Sub string
	Legend      bool
}

// DonutOption builds a donut chart. Zero-valued slices are dropped.
func DonutOption(in DonutInput) map[string]any {
	var data []map[string]any
	for _, s := range in.Slices {
		if s.Value <= 0 {
			continue
		}
		data = append(data, map[string]any{
			"name": s.Label, "value": math.Round(s.Value*10) / 10, "itemStyle": map[string]any{"color": s.Color},
		})
	}
	centreY := "50%"
	if in.Legend {
		centreY = "44%"
	}
	opt := map[string]any{
		"tooltip": map[string]any{"trigger": "item", "formatter": "{b}: {c}%"},
		"series": []map[string]any{{
			"type": "pie", "radius": []string{"58%", "82%"}, "center": []string{"50%", centreY},
			"avoidLabelOverlap": true, "label": map[string]any{"show": false},
			"itemStyle": map[string]any{"borderWidth": 2, "borderColor": "transparent", "borderRadius": 3},
			"emphasis":  map[string]any{"scaleSize": 4},
			"data":      data,
		}},
	}
	if in.Centre != "" {
		opt["title"] = map[string]any{
			"text": in.Centre, "subtext": in.Sub, "left": "center", "top": centreY, "itemGap": 2,
			"textVerticalAlign": "middle",
			"textStyle":         map[string]any{"fontSize": 22, "fontWeight": 600},
			"subtextStyle":      map[string]any{"fontSize": 12},
		}
	}
	if in.Legend {
		opt["legend"] = map[string]any{"bottom": 0, "icon": "circle", "itemWidth": 8, "itemHeight": 8}
	}
	return opt
}

// AGPPoint is one time-of-day bin of an ambulatory glucose profile, in mg/dL.
// Minute is minutes since midnight.
type AGPPoint struct {
	Minute                 int
	P5, P25, P50, P75, P95 float64
}

// AGPInput describes an AGP; Low and High are the target range in mg/dL.
type AGPInput struct {
	Points    []AGPPoint
	Low, High float64
	Unit      render.Unit
}

// AGPOption builds an ambulatory glucose profile: the median line over 25-75
// and 5-95 percentile bands, with the target range shaded. The bands are
// stacked areas; each data item is [minute, stackedValue, realValue] and the
// "gv:rawaxis" tooltip formatter shows the real value.
func AGPOption(in AGPInput) map[string]any {
	u := in.Unit
	base, lo, mid, hi, med := [][]any{}, [][]any{}, [][]any{}, [][]any{}, [][]any{}
	top := 0.0
	for _, p := range in.Points {
		m := p.Minute
		p5, p25, p50, p75, p95 := conv(p.P5, u), conv(p.P25, u), conv(p.P50, u), conv(p.P75, u), conv(p.P95, u)
		top = math.Max(top, p95)
		base = append(base, []any{m, p5, p5})
		lo = append(lo, []any{m, round2(p25 - p5), p25})
		mid = append(mid, []any{m, round2(p75 - p25), p75})
		hi = append(hi, []any{m, round2(p95 - p75), p95})
		med = append(med, []any{m, p50, p50})
	}
	band := func(name string, data [][]any, color string) map[string]any {
		return map[string]any{
			"name": name, "type": "line", "stack": "agp", "data": data, "symbol": "none", "smooth": 0.3,
			"lineStyle": map[string]any{"opacity": 0}, "areaStyle": map[string]any{"color": color},
			"emphasis": map[string]any{"disabled": true}, "z": 1,
		}
	}
	lowV, highV := conv(in.Low, u), conv(in.High, u)
	return map[string]any{
		"grid":    grid(12, 12, 28, 8),
		"tooltip": map[string]any{"trigger": "axis", "formatter": "gv:rawaxis:" + glucoseFmt(u)[3:]},
		"xAxis": map[string]any{
			"type": "value", "min": 0, "max": 1440, "interval": 180,
			"axisLabel": map[string]any{"formatter": fmtHHMM},
			"splitLine": map[string]any{"show": false},
		},
		"yAxis": glucoseAxis(u, highV, highV, top),
		"series": []map[string]any{
			{
				"name": "5th percentile", "type": "line", "stack": "agp", "data": base, "symbol": "none", "smooth": 0.3,
				"lineStyle": map[string]any{"opacity": 0}, "emphasis": map[string]any{"disabled": true},
			},
			band("25th percentile", lo, colBand),
			band("75th percentile", mid, colBandIn),
			band("95th percentile", hi, colBand),
			{
				"name": "Median", "type": "line", "data": med, "symbol": "none", "smooth": 0.3, "z": 3,
				"lineStyle": map[string]any{"width": 2.5, "color": colLine}, "itemStyle": map[string]any{"color": colLine},
				"markArea": map[string]any{
					"silent": true, "itemStyle": map[string]any{"color": colTarget},
					"data": [][]map[string]any{{{"yAxis": lowV}, {"yAxis": highV}}},
				},
				"markLine": map[string]any{
					"silent": true, "symbol": "none", "label": map[string]any{"show": false},
					"lineStyle": map[string]any{"color": ColInRange, "type": "dashed", "width": 1},
					"data":      []map[string]any{{"yAxis": lowV}, {"yAxis": highV}},
				},
			},
		},
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// HeatCell is one hour of one day: Avg is a mean glucose in mg/dL. Day is a
// row index into HeatmapInput.Days, Hour is 0-23.
type HeatCell struct {
	Day, Hour int
	Avg       float64
}

// HeatmapInput describes a day-by-hour glucose heatmap. Days are the row
// labels, top to bottom. Hours without a cell stay blank.
type HeatmapInput struct {
	Days      []string
	Cells     []HeatCell
	Low, High float64
	Unit      render.Unit
}

// HeatmapOption builds a day-by-hour heatmap coloured by range: below, in and
// above target.
func HeatmapOption(in HeatmapInput) map[string]any {
	u := in.Unit
	hours := make([]string, 24)
	for h := range hours {
		hours[h] = fmt.Sprintf("%02d", h)
	}
	data := make([]map[string]any, 0, len(in.Cells))
	for _, c := range in.Cells {
		if c.Day < 0 || c.Day >= len(in.Days) || c.Hour < 0 || c.Hour > 23 {
			continue
		}
		data = append(data, map[string]any{
			"name":  fmt.Sprintf("%s %02d:00", in.Days[c.Day], c.Hour),
			"value": []any{c.Hour, c.Day, conv(c.Avg, u)},
		})
	}
	lowV, highV := conv(in.Low, u), conv(in.High, u)
	return map[string]any{
		"grid":    grid(8, 8, 24, 8),
		"tooltip": map[string]any{"trigger": "item", "formatter": "gv:cell:" + glucoseFmt(u)[3:]},
		"xAxis":   map[string]any{"type": "category", "data": hours, "splitArea": map[string]any{"show": false}},
		"yAxis":   map[string]any{"type": "category", "data": in.Days, "inverse": true, "axisTick": map[string]any{"show": false}},
		"visualMap": map[string]any{
			"show": false, "type": "piecewise", "dimension": 2,
			"pieces": []map[string]any{
				{"lt": lowV, "color": ColLow},
				{"gte": lowV, "lte": highV, "color": ColInRange},
				{"gt": highV, "color": ColHigh},
			},
		},
		"series": []map[string]any{{
			"type": "heatmap", "data": data,
			"itemStyle": map[string]any{"borderWidth": 2, "borderColor": "transparent", "borderRadius": 3},
			"emphasis":  map[string]any{"itemStyle": map[string]any{"borderColor": "#888"}},
		}},
	}
}

// TrendPoint is one value at a time: T is Unix milliseconds, V a glucose in
// mg/dL, or the plain value when the input's Plain is set.
type TrendPoint struct {
	T int64
	V float64
}

// TrendSeries is one line of a TrendInput.
type TrendSeries struct {
	Name   string
	Color  string // empty picks the default
	Points []TrendPoint
	Area   bool
}

// TrendInput describes a time-axis line chart with optional zoom and target
// range. With Plain set, values are used as they are, formatted with Format
// (a formatter name such as "gv:pct"), and no target range is drawn.
type TrendInput struct {
	Series    []TrendSeries
	Low, High float64
	Unit      render.Unit
	Zoom      bool
	Plain     bool
	Format    string
}

// TrendOption builds a time-axis line chart, e.g. daily average glucose or a
// multi-day trace.
func TrendOption(in TrendInput) map[string]any {
	u := in.Unit
	f := glucoseFmt(u)
	if in.Plain {
		f = in.Format
		if f == "" {
			f = fmtInt
		}
	}
	top := 0.0
	series := make([]map[string]any, 0, len(in.Series))
	for i, s := range in.Series {
		data := make([][]any, 0, len(s.Points))
		for _, p := range s.Points {
			v := p.V
			if !in.Plain {
				v = conv(v, u)
			}
			top = math.Max(top, v)
			data = append(data, []any{p.T, v})
		}
		it := map[string]any{
			"name": s.Name, "type": "line", "data": data, "symbol": "none", "smooth": 0.2,
			"lineStyle": map[string]any{"width": 2},
		}
		if s.Color != "" {
			it["itemStyle"] = map[string]any{"color": s.Color}
			it["lineStyle"] = map[string]any{"width": 2, "color": s.Color}
		}
		if s.Area {
			it["areaStyle"] = map[string]any{"opacity": 0.12}
		}
		if i == 0 && !in.Plain && in.High > 0 {
			it["markArea"] = map[string]any{
				"silent": true, "itemStyle": map[string]any{"color": colTarget},
				"data": [][]map[string]any{{{"yAxis": conv(in.Low, u)}, {"yAxis": conv(in.High, u)}}},
			}
		}
		series = append(series, it)
	}
	yAxis := map[string]any{"type": "value", "scale": true, "axisLabel": map[string]any{"formatter": f}}
	if !in.Plain {
		yAxis = glucoseAxis(u, conv(in.High, u), conv(in.High, u), top)
	}
	opt := map[string]any{
		"grid":    grid(16, 12, 28, 8),
		"tooltip": map[string]any{"trigger": "axis", "valueFormatter": f},
		"xAxis":   map[string]any{"type": "time", "splitLine": map[string]any{"show": false}},
		"yAxis":   yAxis,
		"series":  series,
	}
	gridTop, gridBottom := 16, 28
	if len(in.Series) > 1 {
		opt["legend"] = map[string]any{"top": 0, "right": 0, "icon": "roundRect", "itemHeight": 8}
		gridTop = 32
	}
	if in.Zoom {
		gridBottom = 64
		opt["dataZoom"] = []map[string]any{
			{"type": "inside", "filterMode": "none"},
			{"type": "slider", "height": 22, "bottom": 8, "filterMode": "none"},
		}
	}
	opt["grid"] = grid(gridTop, 12, gridBottom, 8)
	return opt
}

// BarSeries is one series of a BarInput. Series sharing a non-empty Stack
// stack on top of each other.
type BarSeries struct {
	Name   string
	Color  string
	Stack  string
	Values []float64 // one per category
}

// BarInput describes a bar chart. Format is a formatter name for the values
// ("gv:pct", "gv:mgdl", "gv:int", "gv:min"); empty means "gv:int". With
// Horizontal set, categories run down the left. Percent-stacked splits, such
// as time in range per activity, use Format "gv:pct" and Max 100.
type BarInput struct {
	Categories []string
	Series     []BarSeries
	Format     string
	Horizontal bool
	Max        float64 // 0 leaves the axis automatic
	Legend     bool
}

// BarOption builds a (stacked, horizontal) bar chart.
func BarOption(in BarInput) map[string]any {
	f := in.Format
	if f == "" {
		f = fmtInt
	}
	series := make([]map[string]any, 0, len(in.Series))
	for _, s := range in.Series {
		it := map[string]any{
			"name": s.Name, "type": "bar", "data": s.Values, "barMaxWidth": 28,
			"itemStyle": map[string]any{"borderRadius": 2},
		}
		if s.Color != "" {
			it["itemStyle"] = map[string]any{"color": s.Color, "borderRadius": 2}
		}
		if s.Stack != "" {
			it["stack"] = s.Stack
			it["itemStyle"].(map[string]any)["borderRadius"] = 0
		}
		series = append(series, it)
	}
	cat := map[string]any{"type": "category", "data": in.Categories, "axisTick": map[string]any{"show": false}}
	val := map[string]any{"type": "value", "axisLabel": map[string]any{"formatter": f}}
	if in.Max > 0 {
		val["max"] = in.Max
	}
	opt := map[string]any{
		"grid":    grid(12, 16, 8, 8),
		"tooltip": map[string]any{"trigger": "axis", "axisPointer": map[string]any{"type": "shadow"}, "valueFormatter": f},
		"series":  series,
	}
	if in.Horizontal {
		cat["inverse"] = true
		opt["xAxis"], opt["yAxis"] = val, cat
	} else {
		opt["xAxis"], opt["yAxis"] = cat, val
	}
	if in.Legend {
		opt["legend"] = map[string]any{"top": 0, "right": 0, "icon": "roundRect", "itemHeight": 8}
		opt["grid"] = grid(32, 16, 8, 8)
	}
	return opt
}

// ScatterPoint is one dot: Label shows in the tooltip.
type ScatterPoint struct {
	X, Y  float64
	Label string
}

// ScatterInput describes a scatter plot. XFormat and YFormat are formatter
// names for the axes and tooltip, e.g. "gv:mgdl" or "gv:min".
type ScatterInput struct {
	Points           []ScatterPoint
	XName, YName     string
	XFormat, YFormat string
	Color            string
}

// ScatterOption builds a scatter plot, e.g. pace against average glucose.
func ScatterOption(in ScatterInput) map[string]any {
	xf, yf := in.XFormat, in.YFormat
	if xf == "" {
		xf = "gv:num"
	}
	if yf == "" {
		yf = "gv:num"
	}
	col := in.Color
	if col == "" {
		col = colLine
	}
	data := make([]map[string]any, 0, len(in.Points))
	for _, p := range in.Points {
		data = append(data, map[string]any{"name": p.Label, "value": []float64{p.X, p.Y}})
	}
	axis := func(name, f string) map[string]any {
		return map[string]any{
			"type": "value", "name": name, "scale": true, "nameGap": 8,
			"axisLabel": map[string]any{"formatter": f},
		}
	}
	xAxis := axis(in.XName, xf)
	xAxis["nameLocation"], xAxis["nameGap"] = "middle", 28
	return map[string]any{
		"grid":    grid(28, 16, 36, 8),
		"tooltip": map[string]any{"trigger": "item", "formatter": "gv:point:" + xf[3:] + ":" + yf[3:]},
		"xAxis":   xAxis,
		"yAxis":   axis(in.YName, yf),
		"series": []map[string]any{{
			"type": "scatter", "data": data, "symbolSize": 9,
			"itemStyle": map[string]any{"color": col, "opacity": 0.75},
		}},
	}
}
