package store

import (
	"encoding/json"
	"maps"
	"strings"

	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/i18n"
)

// The settings of the live glucose sources are generic: Config holds the
// chosen source and one flat map of "id.field" values, and the glucose package
// says which fields exist. A field with a LegacyKey (Dexcom's) lives in its own
// old Config field instead, so nothing that already stored it moves.

func sourceMapKey(id string, f glucose.Field) string { return id + "." + f.Key }

// SourceValue returns the current value of a non-secret field of source id.
func (c Config) SourceValue(id string, f glucose.Field) string {
	if f.LegacyKey != "" {
		v, _ := c.Get(f.LegacyKey)
		return v
	}
	if v, ok := c.Sources[sourceMapKey(id, f)]; ok {
		return v
	}
	return f.Default
}

// SetSourceValue changes a non-secret field of source id. The map is copied, so
// a Config taken from LoadConfig can be changed without touching shared state.
func (c *Config) SetSourceValue(id string, f glucose.Field, v string) error {
	if f.LegacyKey != "" {
		return c.Set(f.LegacyKey, v)
	}
	m := maps.Clone(c.Sources)
	if m == nil {
		m = map[string]string{}
	}
	m[sourceMapKey(id, f)] = v
	c.Sources = m
	return nil
}

// sourceConfigJSON and parseSourceConfig convert the Sources map to and from
// the source_config column.
func sourceConfigJSON(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, _ := json.Marshal(m) // a map of strings cannot fail
	return string(b)
}

func parseSourceConfig(s string) map[string]string {
	if s == "" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(s), &m) != nil {
		return nil
	}
	return m
}

// sourceConfigKeys adds one scriptable key per non-secret source field that has
// no legacy key, plus "glucose_source" itself.
func sourceConfigKey(name string) (configKey, bool) {
	if name == "glucose_source" {
		return strKey(func(c *Config) *string { return &c.GlucoseSource }), true
	}
	for _, d := range glucose.All() {
		for _, f := range d.Fields {
			if f.Secret() || f.LegacyKey != "" || glucose.SettingKey(d.ID, f) != name {
				continue
			}
			id, field := d.ID, f
			return configKey{
				get: func(c *Config) string { return c.SourceValue(id, field) },
				set: func(c *Config, v string) error { return c.SetSourceValue(id, field, v) },
			}, true
		}
	}
	return configKey{}, false
}

func sourceConfigKeyNames() []string {
	out := []string{"glucose_source"}
	for _, d := range glucose.All() {
		for _, f := range d.Fields {
			if !f.Secret() && f.LegacyKey == "" {
				out = append(out, glucose.SettingKey(d.ID, f))
			}
		}
	}
	return out
}

// checkSources validates the glucose source settings.
func (c Config) checkSources() (Problem, bool) {
	if c.GlucoseSource != "" {
		if _, ok := glucose.Lookup(c.GlucoseSource); !ok {
			return problem(i18n.Key("validate.source"), "source", c.GlucoseSource, "known", strings.Join(glucose.IDs(), ", ")), false
		}
	}
	for _, d := range glucose.All() {
		if p, ok := glucose.Validate(d.ID, func(f glucose.Field) string { return c.SourceValue(d.ID, f) }); !ok {
			return problem(p.Key, p.Args...), false
		}
	}
	return Problem{}, true
}
