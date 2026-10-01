package web

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/store"
)

// Helpers for the Overview, the activity page, the dashboard and the charts:
// the translator of a data struct, locale-aware formats, and the code-to-key
// tables for texts the analytics packages hand over as codes.

// trCtx is the translator the withTranslator middleware put on ctx, English
// when there is none (a data builder called from a test or a background job).
func trCtx(ctx context.Context) *i18n.Translator {
	if tr, ok := ctx.Value(trKey{}).(*i18n.Translator); ok {
		return tr
	}
	return i18n.English()
}

func orEnglish(tr *i18n.Translator) *i18n.Translator {
	if tr == nil {
		return i18n.English()
	}
	return tr
}

func (d StatsData) tr() *i18n.Translator    { return orEnglish(d.T) }
func (d DashData) tr() *i18n.Translator     { return orEnglish(d.T) }
func (d ActivityData) tr() *i18n.Translator { return orEnglish(d.T) }

// ---- numbers, durations, dates

// fmtDurationT is fmtDuration in the translator's language.
func fmtDurationT(tr *i18n.Translator, d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 60 {
		return tr.T("fmt.duration.min", "n", m)
	}
	return tr.T("fmt.duration.hm", "h", m/60, "m", pad2(m%60))
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// fmtAgoT is "12 min ago" / "vor 12 Min.".
func fmtAgoT(tr *i18n.Translator, d time.Duration) string {
	return tr.T("fmt.ago", "duration", fmtDurationT(tr, d))
}

// pctT is v as a percentage with the locale's decimal mark.
func pctT(tr *i18n.Translator, v float64, decimals int) string {
	return tr.Num(v, decimals) + "%"
}

// dayMonthT is "2 Jan" in loc.
func dayMonthT(tr *i18n.Translator, t time.Time, loc *time.Location) string {
	return tr.Date(t.In(loc), false)
}

// rangeTextT is "16 Sep – 30 Sep".
func rangeTextT(tr *i18n.Translator, from, to time.Time, loc *time.Location) string {
	return tr.T("fmt.range", "from", dayMonthT(tr, from, loc), "to", dayMonthT(tr, to, loc))
}

// weekdaysMondayFirst are the short weekday names, Monday first.
func weekdaysMondayFirst(tr *i18n.Translator) []string {
	out := make([]string, 7)
	for i := range out {
		out[i] = tr.Weekday(time.Weekday((i+1)%7), false)
	}
	return out
}

// glucoseDec is how many decimals a glucose value has in unit u.
func glucoseDec(u render.Unit) int {
	if u == render.MmolL {
		return 1
	}
	return 0
}

// signedGlucoseT is signedGlucose with the locale's decimal mark.
func signedGlucoseT(tr *i18n.Translator, v float64, u render.Unit) string {
	x := v * unitScale(u)
	if x >= 0 {
		return "+" + tr.Num(x, glucoseDec(u))
	}
	return "−" + tr.Num(-x, glucoseDec(u))
}

// valueT is render.Value with the locale's decimal mark.
func valueT(tr *i18n.Translator, mgdl float64, u render.Unit) string {
	return tr.Num(mgdl*unitScale(u), glucoseDec(u))
}

// ---- code tables (keys are registered with i18n.Key, looked up dynamically)

var trendKeys = map[string]string{
	"steady":          i18n.Key("trend.steady"),
	"rising":          i18n.Key("trend.rising"),
	"falling":         i18n.Key("trend.falling"),
	"rising_quickly":  i18n.Key("trend.rising_quickly"),
	"falling_quickly": i18n.Key("trend.falling_quickly"),
	"rising_fast":     i18n.Key("trend.rising_fast"),
	"falling_fast":    i18n.Key("trend.falling_fast"),
}

// trendLabelT is the trend's words ("rising fast") in the translator's language.
func trendLabelT(tr *i18n.Translator, t analytics.Trend) string {
	if key, ok := trendKeys[t.Code]; ok {
		return tr.T(key) // i18n:dynamic
	}
	return t.Label
}

var rangeKeys = map[string]string{
	"7d":  i18n.Key("range.7d"),
	"14d": i18n.Key("range.14d"),
	"30d": i18n.Key("range.30d"),
	"90d": i18n.Key("range.90d"),
	"all": i18n.Key("range.all"),
}

// rangeLabelT names a range preset.
func rangeLabelT(tr *i18n.Translator, key string) string {
	if k, ok := rangeKeys[key]; ok {
		return tr.T(k) // i18n:dynamic
	}
	return statsRangeLabels[key]
}

var rangeNoteKeys = map[string]string{
	"missing": i18n.Key("range.note.missing"),
	"format":  i18n.Key("range.note.format"),
	"order":   i18n.Key("range.note.order"),
	"future":  i18n.Key("range.note.future"),
	"span":    i18n.Key("range.note.span"),
}

var zoneKeys = map[string]string{
	"A": i18n.Key("kpi.zone.A"), "B": i18n.Key("kpi.zone.B"), "C": i18n.Key("kpi.zone.C"),
	"D": i18n.Key("kpi.zone.D"), "E": i18n.Key("kpi.zone.E"),
}

var partKeys = map[string]string{
	"Night":     i18n.Key("overview.dayparts.night"),
	"Morning":   i18n.Key("overview.dayparts.morning"),
	"Afternoon": i18n.Key("overview.dayparts.afternoon"),
	"Evening":   i18n.Key("overview.dayparts.evening"),
}

// partNameT translates a part-of-day name from analytics.
func partNameT(tr *i18n.Translator, name string) string {
	if k, ok := partKeys[name]; ok {
		return tr.T(k) // i18n:dynamic
	}
	return name
}

// bandNames are the five band names, very low to very high.
func bandNames(tr *i18n.Translator) (veryLow, low, inRange, high, veryHigh string) {
	return tr.T("band.very_low"), tr.T("band.low"), tr.T("band.in_range"), tr.T("band.high"), tr.T("band.very_high")
}

// ---- Overview card definitions (titles, help and options live in store)

var cardTextKeys = map[string]string{
	"kpis.title":              i18n.Key("overview.card.kpis.title"),
	"kpis.help":               i18n.Key("overview.card.kpis.help"),
	"tir.title":               i18n.Key("overview.card.tir.title"),
	"tir.help":                i18n.Key("overview.card.tir.help"),
	"agp.title":               i18n.Key("overview.card.agp.title"),
	"agp.help":                i18n.Key("overview.card.agp.help"),
	"agp.opt.hours":           i18n.Key("overview.card.agp.opt.hours"),
	"agp.opt.hours.all":       i18n.Key("overview.card.agp.opt.hours.all"),
	"agp.opt.hours.rest":      i18n.Key("overview.card.agp.opt.hours.rest"),
	"trend.title":             i18n.Key("overview.card.trend.title"),
	"trend.help":              i18n.Key("overview.card.trend.help"),
	"heatmap.title":           i18n.Key("overview.card.heatmap.title"),
	"heatmap.help":            i18n.Key("overview.card.heatmap.help"),
	"heatmap.opt.metric":      i18n.Key("overview.card.heatmap.opt.metric"),
	"heatmap.opt.metric.tir":  i18n.Key("overview.card.heatmap.opt.metric.tir"),
	"heatmap.opt.metric.mean": i18n.Key("overview.card.heatmap.opt.metric.mean"),
	"calendar.title":          i18n.Key("overview.card.calendar.title"),
	"calendar.help":           i18n.Key("overview.card.calendar.help"),
	"dayparts.title":          i18n.Key("overview.card.dayparts.title"),
	"dayparts.help":           i18n.Key("overview.card.dayparts.help"),
	"episodes.title":          i18n.Key("overview.card.episodes.title"),
	"episodes.help":           i18n.Key("overview.card.episodes.help"),
	"by_sport.title":          i18n.Key("overview.card.by_sport.title"),
	"by_sport.help":           i18n.Key("overview.card.by_sport.help"),
	"insights.title":          i18n.Key("overview.card.insights.title"),
	"insights.help":           i18n.Key("overview.card.insights.help"),
	"table.title":             i18n.Key("overview.card.table.title"),
	"table.help":              i18n.Key("overview.card.table.help"),
	"sources.title":           i18n.Key("overview.card.sources.title"),
	"sources.help":            i18n.Key("overview.card.sources.help"),
}

// cardText translates part of a card definition, or returns fallback (the
// English text in store) for a card or option this table does not know.
func cardText(tr *i18n.Translator, part, fallback string) string {
	if k, ok := cardTextKeys[part]; ok {
		return tr.T(k) // i18n:dynamic
	}
	return fallback
}

func cardTitleT(tr *i18n.Translator, def store.OverviewCardDef) string {
	return cardText(tr, def.ID+".title", def.Title)
}

func cardHelpT(tr *i18n.Translator, def store.OverviewCardDef) string {
	return cardText(tr, def.ID+".help", def.Help)
}

// ---- suspected sensor artifacts

// artifactLabelT is the short name of a suspected artifact.
func artifactLabelT(tr *i18n.Translator, a analytics.Artifact) string {
	switch {
	case a.Marked:
		return tr.T("artifact.label.marked")
	case a.Kind == analytics.ArtifactCompression:
		return tr.T("artifact.label.compression")
	case a.Kind == analytics.ArtifactDip:
		return tr.T("artifact.label.dip")
	}
	return tr.T("artifact.label.other")
}

// whyNum is an analytics number (mg/dL, or mg/dL per minute) in the user's
// unit. A rate gets one more decimal, because it is small.
func whyNum(tr *i18n.Translator, v float64, u render.Unit, rate bool) string {
	dec := glucoseDec(u)
	if rate {
		dec++
	}
	return tr.Num(v*unitScale(u), dec)
}

// whyText says one reason code in the translator's language. The numbers in w
// are mg/dL; they are shown in unit u. An unknown code falls back to fallback.
func whyText(tr *i18n.Translator, w analytics.Why, u render.Unit, fallback string) string {
	unit := string(u)
	switch w.Code {
	case "compression":
		return tr.T("artifact.reason.compression", "min", int(w.Args["min"]))
	case "dip":
		return tr.T("artifact.reason.dip", "drop", whyNum(tr, w.Args["drop"], u, false), "unit", unit, "n", int(w.Args["n"]))
	case "marked":
		return tr.T("artifact.reason.marked")
	case "fall_rate":
		return tr.T("artifact.why.fall_rate", "rate", whyNum(tr, w.Args["rate"], u, true), "unit", unit)
	case "fast_fall":
		return tr.T("artifact.why.fast_fall")
	case "fast_recovery":
		return tr.T("artifact.why.fast_recovery")
	case "short":
		return tr.T("artifact.why.short")
	case "gap_low":
		return tr.T("artifact.why.gap_low")
	case "decline":
		return tr.T("artifact.why.decline", "drop", whyNum(tr, w.Args["drop"], u, false), "unit", unit)
	case "activity":
		return tr.T("artifact.why.activity")
	case "recurrence":
		return tr.Tn("artifact.why.recurrence", int(w.Args["nights"]))
	case "dropped":
		return tr.T("artifact.why.dropped", "drop", whyNum(tr, w.Args["drop"], u, false), "unit", unit)
	case "gap_dip":
		return tr.T("artifact.why.gap_dip")
	case "fall_rate_fast":
		return tr.T("artifact.why.fall_rate_fast", "rate", whyNum(tr, w.Args["rate"], u, true), "unit", unit)
	case "returned":
		return tr.T("artifact.why.returned")
	case "flat":
		return tr.T("artifact.why.flat")
	}
	return fallback
}

// artifactReasonT is the one-line reason of an artifact.
func artifactReasonT(tr *i18n.Translator, a analytics.Artifact, u render.Unit) string {
	return whyText(tr, a.ReasonWhy, u, a.Reason)
}

// artifactDetailT is analytics.Artifact.Detail in the translator's language:
// the reason, then the factors behind the confidence.
func artifactDetailT(tr *i18n.Translator, a analytics.Artifact, u render.Unit) string {
	reason := artifactReasonT(tr, a, u)
	if len(a.Reasons) == 0 {
		return reason
	}
	parts := make([]string, len(a.Reasons))
	for i, text := range a.Reasons {
		parts[i] = text
		if i < len(a.Why) {
			parts[i] = whyText(tr, a.Why[i], u, text)
		}
	}
	return reason + ": " + strings.Join(parts, "; ")
}

// rangeNoteT is the notice about a custom range that was rejected.
func rangeNoteT(tr *i18n.Translator, code string) string {
	note := code
	if k, ok := rangeNoteKeys[code]; ok {
		note = tr.T(k) // i18n:dynamic
	}
	return tr.T("range.note.fallback", "note", note)
}
