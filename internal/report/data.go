package report

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// Disclaimer is printed in the footer of every page.
const Disclaimer = "Not a medical device. Discuss these numbers with your care team."

// Limits on the long lists, so two pages hold a month of data.
const (
	maxRecentEpisodes = 8
	maxActivities     = 12
)

// tplData is the JSON the Typst template reads (data.json). Every number is
// already formatted in the chosen unit, so the template only lays out text.
type tplData struct {
	Title      string `json:"title"`
	Range      string `json:"range"`
	Days       string `json:"days"`
	Generated  string `json:"generated"`
	Compare    string `json:"compare"`
	Unit       string `json:"unit"`
	Version    string `json:"version"`
	Disclaimer string `json:"disclaimer"`
	HasData    bool   `json:"hasData"`

	Kpis  []tplKPI  `json:"kpis"`
	Bands []tplBand `json:"bands"`

	AGPNote string `json:"agpNote"`

	PartsHead    []string   `json:"partsHead"`
	Parts        [][]string `json:"parts"`
	EpisodesHead []string   `json:"episodesHead"`
	Episodes     [][]string `json:"episodes"`
	RecentHead   []string   `json:"recentHead"`
	Recent       [][]string `json:"recent"`
	SportsHead   []string   `json:"sportsHead"`
	Sports       [][]string `json:"sports"`
	ActsHead     []string   `json:"actsHead"`
	Acts         [][]string `json:"acts"`
	ActsNote     string     `json:"actsNote"`
	SourcesHead  []string   `json:"sourcesHead"`
	Sources      [][]string `json:"sources"`
	Notes        []string   `json:"notes"`
}

type tplKPI struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Sub   string `json:"sub"`
	Delta string `json:"delta"` // "" when not comparing
	Tone  string `json:"tone"`  // good, bad or flat
}

type tplBand struct {
	Label  string `json:"label"`
	Range  string `json:"range"`
	Pct    string `json:"pct"`
	Target string `json:"target"`
	Color  string `json:"color"`
	Met    bool   `json:"met"`
}

// unitScale converts a mg/dL difference into the unit.
func unitScale(u render.Unit) float64 {
	if u == render.MmolL {
		return 1 / stats.MmolFactor
	}
	return 1
}

func (m *Model) val(v float64) string { return render.Value(v, m.unit()) }

func (m *Model) unit() render.Unit {
	if m.In.Unit == "" {
		return render.MgDL
	}
	return m.In.Unit
}

// signed formats a difference with an explicit sign and a real minus.
func signed(v float64, dec int, suffix string) string {
	if math.Abs(v) < math.Pow(10, -float64(dec))/2 {
		return "no change"
	}
	sign := "+"
	if v < 0 {
		sign, v = "−", -v
	}
	return fmt.Sprintf("%s%.*f%s", sign, dec, v, suffix)
}

func pct0(v float64) string { return fmt.Sprintf("%.0f%%", v) }
func pct1(v float64) string { return fmt.Sprintf("%.1f%%", v) }

func dur(d time.Duration) string {
	if d <= 0 {
		return "–"
	}
	mins := int(d.Round(time.Minute) / time.Minute)
	if mins < 60 {
		return fmt.Sprintf("%d min", mins)
	}
	return fmt.Sprintf("%dh %02dm", mins/60, mins%60)
}

// inSubset reports whether the embedded font has a glyph for r. It mirrors the
// --unicodes list in fonts/VENDOR.md.
func inSubset(r rune) bool {
	switch {
	case r >= 0x20 && r <= 0x7E, r >= 0xA0 && r <= 0xFF:
		return true
	}
	return strings.ContainsRune("–—‘’“”•…−≤≥→↑↓≈", r)
}

// clean replaces what the font cannot draw (emoji, non-Latin scripts in an
// activity name) with "?", so a name never prints as blank boxes.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if inSubset(r) {
			return r
		}
		return '?'
	}, s)
}

func clip(s string, n int) string {
	s = clean(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// template builds the template data from the model.
func (m *Model) template() tplData {
	loc := m.In.Loc
	d := tplData{
		Title:      "Glucose report",
		Range:      rangeLabel(m.From, m.To, loc),
		Generated:  "Generated " + m.In.Now.In(loc).Format("2 Jan 2006, 15:04"),
		Unit:       string(m.unit()),
		Version:    "glucava " + m.In.Build,
		Disclaimer: Disclaimer,
		HasData:    m.HasData,
	}
	days := int(math.Round(m.To.Sub(m.From).Hours() / 24))
	d.Days = fmt.Sprintf("%d days", max(days, 1))
	if m.In.Compare && m.Prev != nil {
		d.Compare = "Compared with " + rangeLabel(m.In.PrevFrom, m.In.PrevTo, loc)
		if !m.Delta.Valid {
			d.Compare += " (no readings there)"
		}
	}
	if !m.HasData {
		d.Notes = []string{"There are no glucose readings in this range."}
		d.Kpis = []tplKPI{}
		d.Bands = []tplBand{}
		return d
	}
	d.Kpis = m.kpis()
	d.Bands = m.bands()
	d.AGPNote = fmt.Sprintf("Median with the 25–75%% and 5–95%% bands over %d days, by time of day. The green band is the target range, %s–%s %s.",
		m.AGP.Days, m.val(m.In.Thr.Low), m.val(m.In.Thr.High), m.unit())

	d.PartsHead = []string{"Part of day", "In range", "Average", "CV", "Readings"}
	for _, p := range m.Parts {
		if p.Count == 0 {
			d.Parts = append(d.Parts, []string{fmt.Sprintf("%s %02d–%02d", p.Name, p.FromHour, p.ToHour%24), "–", "–", "–", "0"})
			continue
		}
		d.Parts = append(d.Parts, []string{fmt.Sprintf("%s %02d–%02d", p.Name, p.FromHour, p.ToHour%24),
			pct0(p.TIR.InRange), m.val(p.Avg), pct0(p.CV), fmt.Sprint(p.Count)})
	}

	d.EpisodesHead = []string{"Kind", "Episodes", "At night", "Longest", "Total time", "Lowest / highest"}
	for _, k := range []struct {
		kind  analytics.EpisodeKind
		label string
	}{
		{analytics.KindVeryLow, "Very low (<" + m.val(m.In.Thr.VeryLow) + ")"},
		{analytics.KindLow, "Low (<" + m.val(m.In.Thr.Low) + ")"},
		{analytics.KindHigh, "High (>" + m.val(m.In.Thr.High) + ")"},
		{analytics.KindVeryHigh, "Very high (>" + m.val(m.In.Thr.VeryHigh) + ")"},
	} {
		st := m.EpStats[k.kind]
		if st.Count == 0 {
			d.Episodes = append(d.Episodes, []string{k.label, "0", "–", "–", "–", "–"})
			continue
		}
		d.Episodes = append(d.Episodes, []string{k.label, fmt.Sprint(st.Count), fmt.Sprint(st.Nocturnal),
			dur(st.Longest), dur(st.Total), m.val(st.Extreme)})
	}

	d.RecentHead = []string{"When", "Kind", "Duration", "Lowest / highest", "Around"}
	for _, e := range recentEpisodes(m.Episodes, maxRecentEpisodes) {
		kind := map[analytics.EpisodeKind]string{analytics.KindLow: "Low", analytics.KindHigh: "High"}[e.Kind]
		if e.Kind == analytics.KindLow && e.Extreme < m.In.Thr.VeryLow {
			kind = "Very low"
		}
		if e.Kind == analytics.KindHigh && e.Extreme > m.In.Thr.VeryHigh {
			kind = "Very high"
		}
		when := e.Start.In(loc).Format("Mon 2 Jan, 15:04")
		if e.Nocturnal {
			when += " (night)"
		}
		d.Recent = append(d.Recent, []string{when, kind, dur(e.Duration), m.val(e.Extreme), m.around(e)})
	}

	if len(m.Sports) > 0 {
		d.SportsHead = []string{"Type", "Count", "TIR", "CV", "Change", "Drop", "Lows", "Avg HR", "Distance", "Climb", "Pace"}
		sc := unitScale(m.unit())
		dec := 0
		if m.unit() == render.MmolL {
			dec = 1
		}
		for _, s := range m.Sports {
			pace, dist, hr := "–", "–", "–"
			if p := render.FormatPace(s.Sport, s.TotalDistance, s.Duration); p != "" {
				pace = p
			}
			if s.TotalDistance > 0 {
				dist = fmt.Sprintf("%.1f km", s.TotalDistance/1000)
			}
			if s.AvgHR > 0 {
				hr = fmt.Sprintf("%.0f", s.AvgHR)
			}
			if s.WithData == 0 {
				d.Sports = append(d.Sports, []string{clean(s.Sport), fmt.Sprint(s.Count), "–", "–", "–", "–", "–", hr, dist, fmt.Sprintf("%.0f m", s.TotalElevation), pace})
				continue
			}
			d.Sports = append(d.Sports, []string{clean(s.Sport), fmt.Sprint(s.Count), pct0(s.TIR), pct0(s.CV),
				signed(s.Delta*sc, dec, ""), fmt.Sprintf("%.1f", s.DropRate*sc), pct0(s.PostLowShare), hr, dist,
				fmt.Sprintf("%.0f m", s.TotalElevation), pace})
		}
	}

	if len(m.Acts) > 0 {
		d.ActsHead = []string{"When", "Activity", "Duration", "In range", "Min / max", "Avg"}
		for i, a := range m.Acts {
			if i >= maxActivities {
				break
			}
			tir, mm, avg := "–", "–", "–"
			if a.Summary != nil {
				tir, mm, avg = pct0(a.Summary.TIR), m.val(a.Summary.Min)+" / "+m.val(a.Summary.Max), m.val(a.Summary.Avg)
			}
			d.Acts = append(d.Acts, []string{a.Start.In(loc).Format("Mon 2 Jan, 15:04"), clip(a.Name, 30) + " · " + clean(a.Sport), dur(a.Duration), tir, mm, avg})
		}
		if len(m.Acts) > maxActivities {
			d.ActsNote = fmt.Sprintf("Showing the newest %d of %d activities.", maxActivities, len(m.Acts))
		}
	}

	if len(m.In.Sources) > 0 {
		d.SourcesHead = []string{"Source", "Readings stored", "Newest reading"}
		for _, s := range m.In.Sources {
			d.Sources = append(d.Sources, []string{clean(s.Name), fmt.Sprint(s.Count), s.Latest.In(loc).Format("2 Jan 2006, 15:04")})
		}
	}

	cov := m.Coverage
	note := fmt.Sprintf("Data coverage %s: %d of %d expected readings, %d gap(s) over 30 minutes",
		pct0(cov.Pct), cov.Slots, cov.Expected, len(cov.Gaps))
	if cov.Longest > 0 {
		note += ", the longest " + dur(cov.Longest)
	}
	d.Notes = append(d.Notes, note+".",
		"Time in range and the other numbers are computed from every stored reading in the window, not only the ones around activities.")
	return d
}

func (m *Model) kpis() []tplKPI {
	k, dl, cmp := m.KPIs, m.Delta, m.Prev != nil && m.Delta.Valid
	sc := unitScale(m.unit())
	dec := 0
	if m.unit() == render.MmolL {
		dec = 1
	}
	delta := func(text string, dir int) (string, string) { // dir: +1 higher is better, -1 lower is better, 0 neutral
		if !cmp {
			return "", "flat"
		}
		tone := "flat"
		if dir != 0 && text != "no change" {
			up := !strings.HasPrefix(text, "−")
			if up == (dir > 0) {
				tone = "good"
			} else {
				tone = "bad"
			}
		}
		return text + " vs previous", tone
	}
	mk := func(label, value, sub, dtext string, dir int) tplKPI {
		t, tone := delta(dtext, dir)
		return tplKPI{Label: label, Value: value, Sub: sub, Delta: t, Tone: tone}
	}
	return []tplKPI{
		mk("Time in range", pct0(k.TIR), "target above 70%", signed(dl.TIR, 1, " pts"), +1),
		mk("Average glucose", m.val(k.Avg), string(m.unit()), signed(dl.Avg*sc, dec, ""), 0),
		mk("GMI", pct1(k.GMI), "estimated A1C", signed(dl.GMI, 1, ""), -1),
		mk("Variability (CV)", pct0(k.CV), "target 36% or less", signed(dl.CV, 1, " pts"), -1),
		mk("Risk index (GRI)", fmt.Sprintf("%.0f", k.GRI), "zone "+k.GRIZone+" (A is lowest)", signed(dl.GRI, 1, ""), -1),
		mk("Data coverage", pct0(k.Coverage), fmt.Sprintf("%d days with data", k.Days), signed(dl.Coverage, 1, " pts"), +1),
	}
}

// bands is the five-band table with the consensus targets (Battelino et al.,
// 2019): below 70 under 4%, below 54 under 1%, in range above 70%, above 180
// under 25%, above 250 under 5%.
func (m *Model) bands() []tplBand {
	t, thr := m.TIR, m.In.Thr
	u := m.unit()
	v := m.val
	return []tplBand{
		{"Very low", "below " + v(thr.VeryLow) + " " + string(u), pct1(t.VeryLow), "under 1%", colVeryLow, t.VeryLow < 1},
		{"Low", v(thr.VeryLow) + "–" + v(thr.Low) + " " + string(u), pct1(t.Low), "below range under 4% in total", colLow, t.Below() < 4},
		{"In range", v(thr.Low) + "–" + v(thr.High) + " " + string(u), pct1(t.InRange), "above 70%", colInRange, t.InRange > 70},
		{"High", v(thr.High) + "–" + v(thr.VeryHigh) + " " + string(u), pct1(t.High), "above range under 25% in total", colHigh, t.Above() < 25},
		{"Very high", "above " + v(thr.VeryHigh) + " " + string(u), pct1(t.VeryHigh), "under 5%", colVeryHigh, t.VeryHigh < 5},
	}
}

// around names the activity an episode happened during or within four hours
// after, the window in which exercise-related lows show up.
func (m *Model) around(e analytics.Episode) string {
	for _, a := range m.Acts { // newest first: the latest matching activity
		if e.Start.Before(a.Start) || e.Start.After(a.End().Add(4*time.Hour)) {
			continue
		}
		if e.Start.Before(a.End()) {
			return "during " + clip(a.Name, 22)
		}
		return "after " + clip(a.Name, 22)
	}
	return "–"
}

// recentEpisodes returns up to n of the low and high episodes, newest first.
// Very-low and very-high runs are nested inside the wider ones, so only those
// two kinds are listed and the severity comes from the extreme.
func recentEpisodes(eps []analytics.Episode, n int) []analytics.Episode {
	var out []analytics.Episode
	for _, e := range eps {
		if e.Kind == analytics.KindLow || e.Kind == analytics.KindHigh {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func rangeLabel(from, to time.Time, loc *time.Location) string {
	f, t := from.In(loc), to.In(loc)
	if f.Year() == t.Year() {
		return f.Format("2 Jan") + " – " + t.Format("2 Jan 2006")
	}
	return f.Format("2 Jan 2006") + " – " + t.Format("2 Jan 2006")
}

// Files returns everything the template needs next to it: data.json and the
// chart SVGs, keyed by file name.
func (m *Model) Files() (map[string][]byte, error) {
	data, err := json.MarshalIndent(m.template(), "", " ")
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"data.json": data}
	if m.HasData {
		u, thr, loc := m.unit(), m.In.Thr, m.In.Loc
		files["tir.svg"] = TIRBarSVG(m.TIR)
		files["agp.svg"] = AGPSVG(m.AGP, thr, u)
		files["trend.svg"] = TrendSVG(m.Daily, thr, u, loc)
		files["parts.svg"] = DayPartsSVG(m.Parts)
	}
	return files, nil
}

// Filename is the download name: glucava-report-<from>_<to>.pdf, dates in loc.
func Filename(from, to time.Time, loc *time.Location) string {
	return "glucava-report-" + from.In(loc).Format("2006-01-02") + "_" + to.In(loc).Format("2006-01-02") + ".pdf"
}
