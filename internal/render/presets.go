package render

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

// PresetByID returns the preset with that ID, or ok=false.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
