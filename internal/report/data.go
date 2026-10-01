package report

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// Disclaimer is the English footer text. The PDF prints report.disclaimer
// from the translator; this constant stays for callers that want the plain text.
const Disclaimer = "Not a medical device. Discuss these numbers with your care team."

// Limits on the long lists, so two pages hold a month of data.
const (
	maxRecentEpisodes = 8
	maxActivities     = 12
)

// tplData is the JSON the Typst template reads (data.json). Every number is
// already formatted in the chosen unit and locale, and every label comes from
// the translator through Labels, so the template only lays out text.
type tplData struct {
	Lang      string `json:"lang"`
	Title     string `json:"title"`
	Range     string `json:"range"`
	Days      string `json:"days"`
	Generated string `json:"generated"`
	Compare   string `json:"compare"`
	Unit      string `json:"unit"`
	UnitNote  string `json:"unitNote"`
	Version   string `json:"version"`
	// Disclaimer is the footer text.
	Disclaimer string `json:"disclaimer"`
	HasData    bool   `json:"hasData"`

	// Labels are the fixed texts of the template (section titles, captions).
	Labels map[string]string `json:"labels"`

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

// tr is the translator of the report; English when none was given.
func (m *Model) tr() *i18n.Translator {
	if m.In.T != nil {
		return m.In.T
	}
	return i18n.English()
}

// val formats a glucose value (mg/dL in) for the unit, with the locale's
// decimal separator.
func (m *Model) val(v float64) string {
	if m.unit() == render.MmolL {
		return m.tr().Num(v/stats.MmolFactor, 1)
	}
	return m.tr().Num(v, 0)
}

func (m *Model) unit() render.Unit {
	if m.In.Unit == "" {
		return render.MgDL
	}
	return m.In.Unit
}

// signed formats a difference with an explicit sign and a real minus. The
// second result is +1, -1 or 0 (no visible change).
func (m *Model) signed(v float64, dec int, suffix string) (string, int) {
	if math.Abs(v) < math.Pow(10, -float64(dec))/2 {
		return m.tr().T("report.nochange"), 0
	}
	sign, dir := "+", 1
	if v < 0 {
		sign, v, dir = "−", -v, -1
	}
	return sign + m.tr().Num(v, dec) + suffix, dir
}

func (m *Model) pct0(v float64) string { return m.tr().T("report.pct", "v", m.tr().Num(v, 0)) }
func (m *Model) pct1(v float64) string { return m.tr().T("report.pct", "v", m.tr().Num(v, 1)) }

// dur formats a duration as "25 min" or "1h 05m".
func (m *Model) dur(d time.Duration) string {
	if d <= 0 {
		return "–"
	}
	mins := int(d.Round(time.Minute) / time.Minute)
	if mins < 60 {
		return m.tr().T("report.dur.min", "m", strconv.Itoa(mins))
	}
	return m.tr().T("report.dur.hm", "h", strconv.Itoa(mins/60), "m", fmt.Sprintf("%02d", mins%60))
}

// inSubset reports whether the embedded font has a glyph for r. It mirrors the
// --unicodes list in fonts/VENDOR.md. Latin-1 covers German (ä ö ü Ä Ö Ü ß).
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

// partKeys name the four parts of the day (night, morning, afternoon, evening).
var partKeys = [4]string{i18n.Key("report.part.0"), i18n.Key("report.part.1"), i18n.Key("report.part.2"), i18n.Key("report.part.3")}

func partLabel(tr *i18n.Translator, i int, p analytics.DayPart) string {
	return fmt.Sprintf("%s %02d–%02d", tr.T(partKeys[i]), p.FromHour, p.ToHour%24) // i18n:dynamic
}

// when formats a moment for a table cell ("Wed 30 Sep, 07:00").
func (m *Model) when(t time.Time) string {
	return m.tr().When(t, m.In.Loc, m.In.Now)
}

// dateTime is a full date with the year and the clock time.
func (m *Model) dateTime(t time.Time) string {
	t = t.In(m.In.Loc)
	return m.tr().Date(t, true) + ", " + m.tr().Time(t)
}

// template builds the template data from the model.
func (m *Model) template() tplData {
	tr := m.tr()
	d := tplData{
		Lang:       tr.Lang(),
		Title:      tr.T("report.title"),
		Range:      m.rangeLabel(m.From, m.To),
		Generated:  tr.T("report.generated", "when", m.dateTime(m.In.Now)),
		Unit:       string(m.unit()),
		Version:    "glucava " + m.In.Build,
		Disclaimer: tr.T("report.disclaimer"),
		HasData:    m.HasData,
		Labels:     m.labels(),
	}
	d.UnitNote = tr.T("report.label.allValuesIn", "unit", d.Unit)
	days := int(math.Round(m.To.Sub(m.From).Hours() / 24))
	d.Days = tr.Tn("report.days", max(days, 1))
	if m.In.Compare && m.Prev != nil {
		d.Compare = tr.T("report.compare", "range", m.rangeLabel(m.In.PrevFrom, m.In.PrevTo))
		if !m.Delta.Valid {
			d.Compare += " " + tr.T("report.compare.nodata")
		}
	}
	if !m.HasData {
		d.Notes = []string{tr.T("report.nodata")}
		d.Kpis = []tplKPI{}
		d.Bands = []tplBand{}
		return d
	}
	d.Kpis = m.kpis()
	d.Bands = m.bands()
	d.AGPNote = tr.Tn("report.agp.note", m.AGP.Days, "low", m.val(m.In.Thr.Low), "high", m.val(m.In.Thr.High), "unit", string(m.unit()))

	d.PartsHead = m.heads(i18n.Key("report.head.partOfDay"), i18n.Key("report.head.inRange"), i18n.Key("report.head.average"), i18n.Key("report.head.cv"), i18n.Key("report.head.readings"))
	for i, p := range m.Parts {
		if p.Count == 0 {
			d.Parts = append(d.Parts, []string{partLabel(tr, i, p), "–", "–", "–", "0"})
			continue
		}
		d.Parts = append(d.Parts, []string{partLabel(tr, i, p),
			m.pct0(p.TIR.InRange), m.val(p.Avg), m.pct0(p.CV), tr.Int(p.Count)})
	}

	d.EpisodesHead = m.heads(i18n.Key("report.head.kind"), i18n.Key("report.head.episodes"), i18n.Key("report.head.atNight"), i18n.Key("report.head.longest"), i18n.Key("report.head.totalTime"), i18n.Key("report.head.lowestHighest"))
	for _, k := range []struct {
		kind  analytics.EpisodeKind
		label string
	}{
		{analytics.KindVeryLow, tr.T("report.ep.verylow", "v", m.val(m.In.Thr.VeryLow))},
		{analytics.KindLow, tr.T("report.ep.low", "v", m.val(m.In.Thr.Low))},
		{analytics.KindHigh, tr.T("report.ep.high", "v", m.val(m.In.Thr.High))},
		{analytics.KindVeryHigh, tr.T("report.ep.veryhigh", "v", m.val(m.In.Thr.VeryHigh))},
	} {
		st := m.EpStats[k.kind]
		if st.Count == 0 {
			d.Episodes = append(d.Episodes, []string{k.label, "0", "–", "–", "–", "–"})
			continue
		}
		d.Episodes = append(d.Episodes, []string{k.label, tr.Int(st.Count), tr.Int(st.Nocturnal),
			m.dur(st.Longest), m.dur(st.Total), m.val(st.Extreme)})
	}

	d.RecentHead = m.heads(i18n.Key("report.head.when"), i18n.Key("report.head.kind"), i18n.Key("report.head.duration"), i18n.Key("report.head.lowestHighest"), i18n.Key("report.head.around"))
	for _, e := range recentEpisodes(m.Episodes, maxRecentEpisodes) {
		kind := tr.T("report.kind.low")
		if e.Kind == analytics.KindHigh {
			kind = tr.T("report.kind.high")
		}
		if e.Kind == analytics.KindLow && e.Extreme < m.In.Thr.VeryLow {
			kind = tr.T("report.kind.verylow")
		}
		if e.Kind == analytics.KindHigh && e.Extreme > m.In.Thr.VeryHigh {
			kind = tr.T("report.kind.veryhigh")
		}
		when := m.when(e.Start)
		if e.Nocturnal {
			when += " " + tr.T("report.night")
		}
		d.Recent = append(d.Recent, []string{when, kind, m.dur(e.Duration), m.val(e.Extreme), m.around(e)})
	}

	if len(m.Sports) > 0 {
		d.SportsHead = m.heads(i18n.Key("report.head.type"), i18n.Key("report.head.count"), i18n.Key("report.head.tir"), i18n.Key("report.head.cv"), i18n.Key("report.head.change"), i18n.Key("report.head.drop"), i18n.Key("report.head.lows"), i18n.Key("report.head.avgHR"), i18n.Key("report.head.distance"), i18n.Key("report.head.climb"), i18n.Key("report.head.pace"))
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
				dist = tr.Num(s.TotalDistance/1000, 1) + " km"
			}
			if s.AvgHR > 0 {
				hr = tr.Num(s.AvgHR, 0)
			}
			climb := tr.Num(s.TotalElevation, 0) + " m"
			if s.WithData == 0 {
				d.Sports = append(d.Sports, []string{clean(s.Sport), tr.Int(s.Count), "–", "–", "–", "–", "–", hr, dist, climb, pace})
				continue
			}
			change, _ := m.signed(s.Delta*sc, dec, "")
			d.Sports = append(d.Sports, []string{clean(s.Sport), tr.Int(s.Count), m.pct0(s.TIR), m.pct0(s.CV),
				change, tr.Num(s.DropRate*sc, 1), m.pct0(s.PostLowShare), hr, dist, climb, pace})
		}
	}

	if len(m.Acts) > 0 {
		d.ActsHead = m.heads(i18n.Key("report.head.when"), i18n.Key("report.head.activity"), i18n.Key("report.head.duration"), i18n.Key("report.head.inRange"), i18n.Key("report.head.minMax"), i18n.Key("report.head.avg"))
		for i, a := range m.Acts {
			if i >= maxActivities {
				break
			}
			tir, mm, avg := "–", "–", "–"
			if a.Summary != nil {
				tir, mm, avg = m.pct0(a.Summary.TIR), m.val(a.Summary.Min)+" / "+m.val(a.Summary.Max), m.val(a.Summary.Avg)
			}
			d.Acts = append(d.Acts, []string{m.when(a.Start), clip(a.Name, 30) + " · " + clean(a.Sport), m.dur(a.Duration), tir, mm, avg})
		}
		if len(m.Acts) > maxActivities {
			d.ActsNote = tr.T("report.acts.note", "shown", tr.Int(maxActivities), "total", tr.Int(len(m.Acts)))
		}
	}

	if len(m.In.Sources) > 0 {
		d.SourcesHead = m.heads(i18n.Key("report.head.source"), i18n.Key("report.head.stored"), i18n.Key("report.head.newest"))
		for _, s := range m.In.Sources {
			d.Sources = append(d.Sources, []string{clean(s.Name), tr.Int(int(s.Count)), m.dateTime(s.Latest)})
		}
	}

	cov := m.Coverage
	args := []any{"pct", m.pct0(cov.Pct), "slots", tr.Int(cov.Slots), "expected", tr.Int(cov.Expected),
		"gaps", tr.Tn("report.gaps", len(cov.Gaps)), "longest", m.dur(cov.Longest)}
	note := tr.T("report.note.coverage", args...)
	if cov.Longest > 0 {
		note = tr.T("report.note.coverage.longest", args...)
	}
	d.Notes = append(d.Notes, note, tr.T("report.note.computed"))
	return d
}

// labels are the fixed texts of the template: section titles and captions.
func (m *Model) labels() map[string]string {
	tr := m.tr()
	return map[string]string{
		"timeInRange":  tr.T("report.label.timeInRange"),
		"tirSub":       tr.T("report.label.tirSub"),
		"agp":          tr.T("report.label.agp"),
		"dayByDay":     tr.T("report.label.dayByDay"),
		"dayByDaySub":  tr.T("report.label.dayByDaySub"),
		"timeOfDay":    tr.T("report.label.timeOfDay"),
		"timeOfDaySub": tr.T("report.label.timeOfDaySub"),
		"lowsHighs":    tr.T("report.label.lowsHighs"),
		"lowsHighsSub": tr.T("report.label.lowsHighsSub"),
		"mostRecent":   tr.T("report.label.mostRecent"),
		"byType":       tr.T("report.label.byType"),
		"byTypeSub":    tr.T("report.label.byTypeSub"),
		"activities":   tr.T("report.label.activities"),
		"sources":      tr.T("report.label.sources"),
		"met":          tr.T("report.label.met"),
		"notMet":       tr.T("report.label.notMet"),
	}
}

// heads translates a table header row; keys are marked with i18n.Key.
func (m *Model) heads(keys ...string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = m.tr().T(k) // i18n:dynamic (every call passes i18n.Key literals)
	}
	return out
}

func (m *Model) kpis() []tplKPI {
	k, dl, cmp := m.KPIs, m.Delta, m.Prev != nil && m.Delta.Valid
	tr := m.tr()
	sc := unitScale(m.unit())
	dec := 0
	if m.unit() == render.MmolL {
		dec = 1
	}
	// dir: +1 higher is better, -1 lower is better, 0 neutral
	mk := func(label, value, sub string, dv float64, dd int, suffix string, dir int) tplKPI {
		t := tplKPI{Label: label, Value: value, Sub: sub, Tone: "flat"}
		if !cmp {
			return t
		}
		text, sign := m.signed(dv, dd, suffix)
		t.Delta = tr.T("report.vsprev", "delta", text)
		if dir != 0 && sign != 0 {
			if (sign > 0) == (dir > 0) {
				t.Tone = "good"
			} else {
				t.Tone = "bad"
			}
		}
		return t
	}
	pts := " " + tr.T("report.pts")
	return []tplKPI{
		mk(tr.T("report.kpi.tir"), m.pct0(k.TIR), tr.T("report.kpi.tir.sub"), dl.TIR, 1, pts, +1),
		mk(tr.T("report.kpi.avg"), m.val(k.Avg), string(m.unit()), dl.Avg*sc, dec, "", 0),
		mk(tr.T("report.kpi.gmi"), m.pct1(k.GMI), tr.T("report.kpi.gmi.sub"), dl.GMI, 1, "", -1),
		mk(tr.T("report.kpi.cv"), m.pct0(k.CV), tr.T("report.kpi.cv.sub"), dl.CV, 1, pts, -1),
		mk(tr.T("report.kpi.gri"), tr.Num(k.GRI, 0), tr.T("report.kpi.gri.sub", "zone", k.GRIZone), dl.GRI, 1, "", -1),
		mk(tr.T("report.kpi.coverage"), m.pct0(k.Coverage), tr.Tn("report.kpi.coverage.sub", k.Days), dl.Coverage, 1, pts, +1),
	}
}

// bands is the five-band table with the consensus targets (Battelino et al.,
// 2019): below 70 under 4%, below 54 under 1%, in range above 70%, above 180
// under 25%, above 250 under 5%.
func (m *Model) bands() []tplBand {
	t, thr, tr := m.TIR, m.In.Thr, m.tr()
	u := string(m.unit())
	v := m.val
	return []tplBand{
		{tr.T("report.band.verylow"), tr.T("report.range.below", "v", v(thr.VeryLow), "unit", u), m.pct1(t.VeryLow), tr.T("report.target.verylow"), colVeryLow, t.VeryLow < 1},
		{tr.T("report.band.low"), v(thr.VeryLow) + "–" + v(thr.Low) + " " + u, m.pct1(t.Low), tr.T("report.target.low"), colLow, t.Below() < 4},
		{tr.T("report.band.inrange"), v(thr.Low) + "–" + v(thr.High) + " " + u, m.pct1(t.InRange), tr.T("report.target.inrange"), colInRange, t.InRange > 70},
		{tr.T("report.band.high"), v(thr.High) + "–" + v(thr.VeryHigh) + " " + u, m.pct1(t.High), tr.T("report.target.high"), colHigh, t.Above() < 25},
		{tr.T("report.band.veryhigh"), tr.T("report.range.above", "v", v(thr.VeryHigh), "unit", u), m.pct1(t.VeryHigh), tr.T("report.target.veryhigh"), colVeryHigh, t.VeryHigh < 5},
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
			return m.tr().T("report.around.during", "name", clip(a.Name, 22))
		}
		return m.tr().T("report.around.after", "name", clip(a.Name, 22))
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

func (m *Model) rangeLabel(from, to time.Time) string {
	tr, loc := m.tr(), m.In.Loc
	f, t := from.In(loc), to.In(loc)
	if f.Year() == t.Year() {
		return tr.Date(f, false) + " – " + tr.Date(t, true)
	}
	return tr.Date(f, true) + " – " + tr.Date(t, true)
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
		tr := m.tr()
		files["tir.svg"] = TIRBarSVG(m.TIR)
		files["agp.svg"] = AGPSVG(tr, m.AGP, thr, u)
		files["trend.svg"] = TrendSVG(tr, m.Daily, thr, u, loc)
		files["parts.svg"] = DayPartsSVG(tr, m.Parts)
	}
	return files, nil
}

// Filename is the download name: glucava-report-<from>_<to>.pdf, dates in loc.
func Filename(from, to time.Time, loc *time.Location) string {
	return "glucava-report-" + from.In(loc).Format("2006-01-02") + "_" + to.In(loc).Format("2006-01-02") + ".pdf"
}
