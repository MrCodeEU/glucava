package web

// Chart builders the Overview needs on top of the general ones in charts.go.

// PctCell is one cell of a percent matrix: X is a column index, Y a row
// index, Pct a value in 0-100, Label the name shown in the tooltip.
type PctCell struct {
	X, Y  int
	Pct   float64
	Label string
}

// PctMatrixInput describes a grid of percentages such as time in range per
// weekday and hour, or per calendar day. XEvery shows only every n-th column
// label, so a long row of week labels stays readable.
type PctMatrixInput struct {
	XLabels, YLabels []string
	Cells            []PctCell
	XEvery           int
}

// tirPieces colour a time-in-range percentage. The scale legend in the
// Overview (tirScaleLegend) repeats these bounds and colours.
var tirPieces = []map[string]any{
	{"lt": 50, "color": ColLow},
	{"gte": 50, "lt": 70, "color": ColHigh},
	{"gte": 70, "lt": 90, "color": "#8fd1b3"},
	{"gte": 90, "color": ColInRange},
}

// PctMatrixOption builds a heatmap of percentages coloured by time-in-range
// band. Cells that are not given stay blank.
func PctMatrixOption(in PctMatrixInput) map[string]any {
	data := make([]map[string]any, 0, len(in.Cells))
	for _, c := range in.Cells {
		if c.X < 0 || c.X >= len(in.XLabels) || c.Y < 0 || c.Y >= len(in.YLabels) {
			continue
		}
		data = append(data, map[string]any{"name": c.Label, "value": []any{c.X, c.Y, round2(c.Pct)}})
	}
	xAxis := map[string]any{"type": "category", "data": in.XLabels, "splitArea": map[string]any{"show": false},
		"axisTick": map[string]any{"show": false}}
	if in.XEvery > 1 {
		xAxis["axisLabel"] = map[string]any{"interval": in.XEvery - 1}
	}
	return map[string]any{
		"grid":    grid(8, 8, 24, 8),
		"tooltip": map[string]any{"trigger": "item", "formatter": "gv:cell:pct"},
		"xAxis":   xAxis,
		"yAxis":   map[string]any{"type": "category", "data": in.YLabels, "inverse": true, "axisTick": map[string]any{"show": false}},
		"visualMap": map[string]any{
			"show": false, "type": "piecewise", "dimension": 2, "pieces": tirPieces,
		},
		"series": []map[string]any{{
			"type": "heatmap", "data": data,
			"itemStyle": map[string]any{"borderWidth": 2, "borderColor": "transparent", "borderRadius": 3},
			"emphasis":  map[string]any{"itemStyle": map[string]any{"borderColor": "#888"}},
		}},
	}
}
