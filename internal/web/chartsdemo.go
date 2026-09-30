package web

import (
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/starfederation/datastar-go/datastar"
	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/render"
)

// chartsDemoEnabled reports whether the chart kitchen sink is served: demo
// mode and dev builds only.
func (s *Server) chartsDemoEnabled() bool { return s.Demo || s.Build == "dev" }

func demoUnit(r *http.Request) render.Unit {
	if r.URL.Query().Get("unit") == "mmol" {
		return render.MmolL
	}
	return render.MgDL
}

// chartsDemoBody renders every chart type from seeded made-up data. A new
// seed changes the data but not the element ids, which is what Datastar's
// morphing has to cope with.
func chartsDemoBody(seed int64, u render.Unit, now time.Time) g.Node {
	rng := rand.New(rand.NewSource(seed))
	const low, high = 70.0, 180.0

	pts := make([]AGPPoint, 0, 96)
	for i := 0; i < 96; i++ {
		m := i * 15
		mid := 118 + 32*math.Sin(float64(m)/1440*2*math.Pi*2-1) + rng.Float64()*6
		pts = append(pts, AGPPoint{Minute: m, P50: mid, P25: mid - 22 - rng.Float64()*6, P75: mid + 26 + rng.Float64()*6,
			P5: mid - 46 - rng.Float64()*8, P95: mid + 62 + rng.Float64()*10})
	}

	days := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	var cells []HeatCell
	for d := range days {
		for h := 0; h < 24; h++ {
			if rng.Intn(14) == 0 {
				continue // a gap in the data
			}
			cells = append(cells, HeatCell{Day: d, Hour: h, Avg: 115 + 45*math.Sin(float64(h)/24*2*math.Pi*2-1) + rng.NormFloat64()*22})
		}
	}

	var avg []TrendPoint
	day := now.Truncate(24*time.Hour).AddDate(0, 0, -59)
	for i := 0; i < 60; i++ {
		t := day.AddDate(0, 0, i).UnixMilli()
		a := 128 + 18*math.Sin(float64(i)/6) + rng.NormFloat64()*6
		avg = append(avg, TrendPoint{T: t, V: a})
	}

	names := []string{"Easy run", "Tempo", "Long run", "Intervals", "Ride", "Walk"}
	var below, inr, above []float64
	var scatter []ScatterPoint
	for i := range names {
		b, a := float64(rng.Intn(14)), float64(rng.Intn(30))
		below, inr, above = append(below, b), append(inr, 100-a-b), append(above, a)
		scatter = append(scatter, ScatterPoint{X: 20 + rng.Float64()*100, Y: 100 + rng.Float64()*80, Label: names[i]})
	}

	tir := []DonutSlice{
		{"Very low", 1, ColVeryLow}, {"Low", 4, ColLow}, {"In range", 71 + float64(rng.Intn(8)), ColInRange},
		{"High", 18, ColHigh}, {"Very high", 4, ColVeryHigh},
	}

	card := func(title string, h int, id string, opt map[string]any) g.Node {
		return Card(H3(g.Text(title)), Chart(id, opt, h))
	}
	return Div(ID("charts-body"),
		Grid("2",
			card("Time in range (donut)", 260, "demo-donut", DonutOption(DonutInput{Slices: tir, Centre: "75%", Sub: "in range", Legend: true})),
			card("Ambulatory glucose profile", 260, "demo-agp", AGPOption(AGPInput{Points: pts, Low: low, High: high, Unit: u})),
		),
		Grid("",
			card("Glucose by day and hour (heatmap)", 280, "demo-heat", HeatmapOption(HeatmapInput{Days: days, Cells: cells, Low: low, High: high, Unit: u})),
		),
		Grid("",
			card("Daily average glucose (trend, zoom)", 320, "demo-trend", TrendOption(TrendInput{Zoom: true, Low: low, High: high, Unit: u,
				Series: []TrendSeries{{Name: "Average", Points: avg, Area: true}}})),
		),
		Grid("2",
			card("Time in range per activity (stacked bar)", 260, "demo-bar", BarOption(BarInput{Categories: names, Format: fmtPct, Max: 100, Horizontal: true, Legend: true,
				Series: []BarSeries{
					{Name: "Below", Color: ColLow, Stack: "t", Values: below},
					{Name: "In range", Color: ColInRange, Stack: "t", Values: inr},
					{Name: "Above", Color: ColHigh, Stack: "t", Values: above},
				}})),
			card("Duration against average glucose (scatter)", 260, "demo-scatter", ScatterOption(ScatterInput{Points: scatter,
				XName: "Duration", YName: "Avg glucose", XFormat: fmtMin, YFormat: glucoseFmt(render.MgDL)})),
		),
	)
}

func (s *Server) chartsDemo(w http.ResponseWriter, r *http.Request) {
	u := demoUnit(r)
	unitLink := func(label, q string) g.Node {
		return A(Href("/ui/charts?unit="+q), g.Attr("data-component", "button"), g.Attr("data-variant", "ghost"), g.Text(label))
	}
	s.html(w, http.StatusOK, Page(s.page(r, "Chart demo", ""),
		PageHead("Chart demo", "Every chart type on made-up data. Demo and dev builds only.",
			unitLink("mg/dL", "mgdl"), unitLink("mmol/L", "mmol"),
			Btn("primary", "New data (morph)", ID("chart-refresh"), g.Attr("data-on:click", "@get('/ui/charts/refresh?unit="+unitQuery(u)+"')")),
		),
		chartsDemoBody(1, u, s.now()),
		ChartScripts(),
	))
}

func unitQuery(u render.Unit) string {
	if u == render.MmolL {
		return "mmol"
	}
	return "mgdl"
}

// chartsDemoRefresh patches new data over the existing chart elements.
func (s *Server) chartsDemoRefresh(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	seed, _ := strconv.ParseInt(r.URL.Query().Get("seed"), 10, 64)
	if seed == 0 {
		seed = s.now().UnixNano()
	}
	_ = sse.PatchElements(renderString(chartsDemoBody(seed, demoUnit(r), s.now())))
}
