package render

import "github.com/MrCodeEU/glucava/internal/i18n"

// Preset is a built-in description template someone can pick instead of
// writing their own. ID is stable and stored (in Config.DescriptionTemplate
// is the raw template text, not the ID — a preset is just a starting point
// the person can still edit afterwards).
type Preset struct {
	ID          string
	Name        string
	Description string
	Template    string
}

// Presets are the built-in choices offered on the settings page, in display
// order. Default reproduces DefaultTemplate exactly, so picking it changes
// nothing; the others show what a template can do, from less text to more.
var Presets = []Preset{
	{
		ID:          "default",
		Name:        "Default",
		Description: `Today's built-in wording: TIR, min/max/avg, and a sparkline.`,
		Template:    DefaultTemplate,
	},
	{
		ID:   "bar",
		Name: "Bar + window",
		Description: `The default line plus a ten-block time-in-range bar (🟥 low, 🟩 in range, 🟨 high), ` +
			`and a second bar for the wider before/after window the chart draws when it differs.`,
		Template: Prefix + `TIR {{.TIR}}% {{.TIRBar}} | min {{.Min}} | max {{.Max}} | avg {{.Avg}} {{.Unit}}` +
			`{{if ne .TIR .TIRWindow}}` + "\n" + `Incl. before/after: {{.TIRWindow}}% {{.TIRWindowBar}}{{end}}` +
			`{{if .Sparkline}}` + "\n" + `{{.Sparkline}}{{end}}`,
	},
	{
		ID:          "minimal",
		Name:        "Minimal",
		Description: `Just the headline number, one line, no sparkline.`,
		Template:    Prefix + `TIR {{.TIR}}%`,
	},
	{
		ID:   "clinical",
		Name: "Clinical",
		Description: `Adds GMI (estimated A1C), coefficient of variation, and the ` +
			`level 2 (severe) hypo/hyperglycemia percentages, on top of the default line.`,
		Template: Prefix + `TIR {{.TIR}}% | min {{.Min}} | max {{.Max}} | avg {{.Avg}} {{.Unit}}` + "\n" +
			`GMI {{.GMI}}% | CV {{.CV}}% | <54: {{.VeryLow}}% | >250: {{.VeryHigh}}%` +
			`{{if .Sparkline}}` + "\n" + `{{.Sparkline}}{{end}}`,
	},
	{
		ID:          "emoji",
		Name:        "Emoji",
		Description: `A friendlier, emoji-led summary.`,
		Template: `🎯 {{.TIR}}% in range  ⬇️ {{.Min}}  ⬆️ {{.Max}}  📊 {{.Avg}} {{.Unit}} avg` +
			`{{if .Sparkline}}` + "\n" + `{{.Sparkline}}{{end}}`,
	},
	{
		ID:          "numbers",
		Name:        "Numbers only",
		Description: `No emoji, no sparkline: a plain sentence for the description.`,
		Template:    `Glucose: {{.Avg}} {{.Unit}} avg (range {{.Min}}-{{.Max}}), {{.TIR}}% in range.`,
	},
}

// presetKeys maps a preset ID to the translation keys of its Name and
// Description. The Template text is deliberately not translated: it is what is
// written to Strava, and IDs and templates stay stable across languages.
var presetKeys = map[string][2]string{
	"default":  {i18n.Key("presets.default.name"), i18n.Key("presets.default.desc")},
	"bar":      {i18n.Key("presets.bar.name"), i18n.Key("presets.bar.desc")},
	"minimal":  {i18n.Key("presets.minimal.name"), i18n.Key("presets.minimal.desc")},
	"clinical": {i18n.Key("presets.clinical.name"), i18n.Key("presets.clinical.desc")},
	"emoji":    {i18n.Key("presets.emoji.name"), i18n.Key("presets.emoji.desc")},
	"numbers":  {i18n.Key("presets.numbers.name"), i18n.Key("presets.numbers.desc")},
}

// PresetText returns the display name and description of the preset in tr's
// language. A preset without keys (a new one not translated yet) falls back to
// the English Name and Description above.
func PresetText(tr *i18n.Translator, id string) (name, desc string) {
	p, ok := PresetByID(id)
	if !ok {
		return "", ""
	}
	if k, has := presetKeys[id]; has {
		return tr.T(k[0]), tr.T(k[1]) // i18n:dynamic (keys registered in presetKeys)
	}
	return p.Name, p.Description
}

// PresetByID returns the preset with that ID, or ok=false.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
