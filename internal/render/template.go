package render

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// TemplateData is what a description template can use. Every numeric field
// is pre-formatted for display (the right decimals, the right unit already
// applied), so a template author never has to know mg/dL from mmol/L or how
// many decimals GMI gets — Block() and RenderBlock() apply that once, here.
type TemplateData struct {
	Sum stats.Summary // the raw numbers, for a template that wants its own formatting

	Unit string // "mg/dL" or "mmol/L"

	TIR, Below, Above string // percent, no "%" suffix
	VeryLow, VeryHigh string // percent, no "%" suffix
	Min, Max, Avg     string // in Unit
	StdDev            string // in Unit
	CV                string // percent, no "%" suffix
	GMI               string // percent, no "%" suffix
	Start, End        string // in Unit

	Sparkline string // "" when disabled or there were no samples to draw
}

func newTemplateData(sum stats.Summary, samples []stats.Sample, opt Options) TemplateData {
	pct := func(v float64) string { return num(v, "") }
	d := TemplateData{
		Sum:      sum,
		Unit:     string(opt.Unit),
		TIR:      pct(sum.TIR),
		Below:    pct(sum.Below),
		Above:    pct(sum.Above),
		VeryLow:  pct(sum.VeryLow),
		VeryHigh: pct(sum.VeryHigh),
		Min:      num(sum.Min, opt.Unit),
		Max:      num(sum.Max, opt.Unit),
		Avg:      num(sum.Avg, opt.Unit),
		StdDev:   num(sum.StdDev, opt.Unit),
		CV:       pct(sum.CV),
		GMI:      pct(sum.GMI),
		Start:    num(sum.Start, opt.Unit),
		End:      num(sum.End, opt.Unit),
	}
	w := opt.SparkWidth
	if w == 0 {
		w = sparkW
	}
	if w > 0 {
		d.Sparkline = stats.Sparkline(samples, w)
	}
	return d
}

// funcMap is available to every description template.
var funcMap = template.FuncMap{
	// round formats v to n decimals, for a template that reaches into .Sum
	// directly instead of using one of the pre-formatted fields above.
	"round": func(v float64, n int) string {
		return fmt.Sprintf("%.*f", n, v)
	},
}

// DefaultTemplate reproduces exactly what Block() has always written: the
// same wording, the same field order, the same conditional sparkline line.
// It is the baseline every preset and every custom template is compared
// against, and TestDefaultTemplateMatchesBlock guards that it never drifts.
const DefaultTemplate = Prefix + `TIR {{.TIR}}% | min {{.Min}} | max {{.Max}} | avg {{.Avg}} {{.Unit}}` +
	`{{if .Sparkline}}` + "\n" + `{{.Sparkline}}{{end}}`

// sampleSummary is fixed, plausible-looking sample data (see stats.Summarize
// for the formulas), used only to dry-run a template someone is about to
// save — never written anywhere. Its numbers are arbitrary; what matters is
// that every field is non-zero, so a template bug that only breaks on a
// zero value (e.g. division) is still caught here.
var sampleSummary = stats.Summary{
	Count: 24, Min: 68, Max: 214, Avg: 132, StdDev: 34.2, CV: 25.9, GMI: 6.5,
	TIR: 78, Below: 8, Above: 14, VeryLow: 2, VeryHigh: 3, Start: 110, End: 140,
}

var sampleSamples = []stats.Sample{{Value: 110}, {Value: 132}, {Value: 140}}

// CheckTemplate reports the first problem with tmplText, or nil: it parses
// the template and dry-runs it against fixed sample data (see
// sampleSummary), the same way store.Config.Validate uses it before a
// custom description template is ever saved. A template that only fails on
// real, in-production data (e.g. a genuinely empty Sparkline) can still slip
// through; RenderBlock's own fallback to DefaultTemplate is the backstop
// for that case.
func CheckTemplate(tmplText string) error {
	_, err := RenderBlock(tmplText, sampleSummary, sampleSamples, Options{})
	return err
}

// RenderBlock renders tmplText against sum/samples/opt and wraps the result
// in the same invisible sentinel and endSentinel Block() uses, so Merge and
// Strip recognise it exactly the same way regardless of which template
// wrote it. tmplText is user-controlled (a custom description template),
// but text/template cannot execute arbitrary code — a bad template only
// ever fails to parse or execute, it cannot escape the sandbox.
func RenderBlock(tmplText string, sum stats.Summary, samples []stats.Sample, opt Options) (string, error) {
	if opt.Unit == "" {
		opt.Unit = MgDL
	}
	t, err := template.New("block").Funcs(funcMap).Parse(tmplText)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := t.Execute(&buf, newTemplateData(sum, samples, opt)); err != nil {
		return "", err
	}
	return sentinel + buf.String() + endSentinel, nil
}
