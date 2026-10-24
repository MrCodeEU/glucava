package web

import (
	"strings"

	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/store"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

// The glucose source card is generated from the descriptors in
// internal/glucose: one dropdown for the active source and, for each source, a
// panel with its fields. A new source needs nothing here.

func bind(name string) g.Node { return g.Attr("data-bind", name) }

// sourceSignalKey names the signal of a field: src.<id>.<key>.
func sourceSignalKey(id, key string) string { return "src." + id + "." + key }

// sourceSignals returns the initial signal values: the chosen source and one
// nested object per source holding its fields. Secrets start empty.
func sourceSignals(cfg store.Config) map[string]any {
	src := map[string]any{}
	for _, d := range glucose.All() {
		vals := map[string]any{}
		for _, f := range d.Fields {
			if f.Secret() {
				vals[f.Key] = ""
			} else {
				vals[f.Key] = cfg.SourceValue(d.ID, f)
			}
		}
		src[d.ID] = vals
	}
	return src
}

func sourceSecretID(id, key string) string { return "src-secret-" + id + "-" + key }

// SourceSecretStatusT is the help line under a secret field of a source. It has
// a stable ID so a save can patch it in place.
func SourceSecretStatusT(tr *i18n.Translator, id, key string, has bool) g.Node {
	help := tr.T("settings.source.secret.unset")
	if has {
		help = tr.T("settings.source.secret.set")
	}
	return Div(ID(sourceSecretID(id, key)), Class("help"), g.Text(help))
}

// sourceFieldT renders one field of a source.
func sourceFieldT(tr *i18n.Translator, id string, f glucose.Field, hasSecret bool) g.Node {
	inputID := "src_" + id + "_" + f.Key
	sig := sourceSignalKey(id, f.Key)
	help := ""
	if f.HelpKey != "" {
		help = tr.T(f.HelpKey) // i18n:dynamic (descriptors register keys with i18n.Key)
	}
	label := tr.T(f.LabelKey) // i18n:dynamic
	switch f.Kind {
	case glucose.KindSelect:
		opts := make([]g.Node, len(f.Options))
		for i, o := range f.Options {
			opts[i] = Option(Value(o.Value), g.Text(tr.T(o.LabelKey))) // i18n:dynamic
		}
		return Field(inputID, label, help, Select(append([]g.Node{ID(inputID), bind(sig)}, opts...)...))
	case glucose.KindPassword:
		return Div(g.Attr("style", "margin-bottom:1rem"),
			Field(inputID, label, help, Input(ID(inputID), Type("password"), AutoComplete("new-password"), bind(sig))),
			SourceSecretStatusT(tr, id, f.Key, hasSecret),
		)
	default:
		return Field(inputID, label, help, Input(ID(inputID), Type("text"), AutoComplete("off"), bind(sig)))
	}
}

// SourceCardT is the Settings card for choosing and configuring the glucose source.
func SourceCardT(tr *i18n.Translator, d SettingsData) g.Node {
	all := glucose.All()
	options := make([]g.Node, len(all))
	panels := make([]g.Node, len(all))
	for i, desc := range all {
		options[i] = Option(Value(desc.ID), g.Text(tr.T(desc.LabelKey))) // i18n:dynamic
		fields := make([]g.Node, len(desc.Fields))
		for j, f := range desc.Fields {
			fields[j] = sourceFieldT(tr, desc.ID, f, d.SourceSecrets[desc.ID+"."+f.Key])
		}
		panels[i] = Div(g.Attr("data-show", "$glucoseSource === '"+desc.ID+"'"),
			g.If(desc.IntroKey != "", P(Class("muted"), g.Text(tr.T(desc.IntroKey)))), // i18n:dynamic
			g.If(desc.Unverified, Notice("warning", g.Text(tr.T("settings.source.unverified")))),
			g.Group(fields),
		)
	}
	return Card(H2(g.Text(tr.T("settings.source.title"))),
		Field("glucoseSource", tr.T("settings.source.label"), "", Select(append([]g.Node{ID("glucoseSource"), bind("glucoseSource")}, options...)...)),
		g.Group(panels),
		Div(append(comp("actions"),
			IndicatorBtn("", tr.T("settings.source.test"), "/actions/glucose/test", "dxtest"),
			IndicatorBtn("", tr.T("settings.source.resync"), "/actions/glucose/resync", "dxresync"))...),
	)
}

// applySourceSignals copies the form's glucose source choice and the
// non-secret field values into cfg. Sources the form does not mention keep what
// cfg has, so a form without the source card cannot blank them.
func applySourceSignals(cfg *store.Config, v settingsSignals) {
	if v.GlucoseSource != "" {
		cfg.GlucoseSource = v.GlucoseSource
	}
	for _, d := range glucose.All() {
		vals, ok := v.Src[d.ID]
		if !ok {
			continue
		}
		for _, f := range d.Fields {
			if val, ok := vals[f.Key]; ok && !f.Secret() {
				_ = cfg.SetSourceValue(d.ID, f, strings.TrimSpace(val))
			}
		}
	}
}

// storeSourceSecrets saves the secret fields the form filled in (an empty one
// keeps the stored value) and returns the signal patch that clears them.
func (s *Server) storeSourceSecrets(v settingsSignals) (map[string]any, error) {
	clear := map[string]any{}
	for _, d := range glucose.All() {
		vals, ok := v.Src[d.ID]
		if !ok {
			continue
		}
		cleared := map[string]any{}
		for _, f := range d.Fields {
			if !f.Secret() {
				continue
			}
			cleared[f.Key] = ""
			if val := vals[f.Key]; val != "" {
				if err := s.Vault.Set(glucose.SecretName(d.ID, f), val); err != nil {
					return nil, err
				}
			}
		}
		clear[d.ID] = cleared
	}
	return clear, nil
}

// sourceSecrets reports which source secrets are stored, keyed "id.field".
func (s *Server) sourceSecrets() map[string]bool {
	out := map[string]bool{}
	for _, d := range glucose.All() {
		for _, f := range d.Fields {
			if f.Secret() {
				_, ok, _ := s.Vault.Get(glucose.SecretName(d.ID, f))
				out[d.ID+"."+f.Key] = ok
			}
		}
	}
	return out
}

// activeSource is the source in use: the Settings choice, else the server's
// default (GLUCAVA_SOURCE, then Dexcom).
func (s *Server) activeSource(cfg store.Config) string {
	if cfg.GlucoseSource != "" {
		return cfg.GlucoseSource
	}
	if s.DefaultSource != "" {
		return s.DefaultSource
	}
	return "dexcom"
}
