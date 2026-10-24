package glucose

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

// A live glucose source describes itself with a Descriptor: its name, the
// settings it needs and how to build a Source from them. Everything else (the
// Settings card, validation, `glucava config`, secret storage, switching the
// active source) is generated from that, so a new source is one file with an
// init() that calls Register, plus its translations. Nothing else changes.

// Kind is how a Field is edited and checked.
type Kind string

const (
	KindText     Kind = "text"     // free text
	KindURL      Kind = "url"      // an http(s) address
	KindSelect   Kind = "select"   // one of Options
	KindPassword Kind = "password" // a secret: write-only, kept in the encrypted vault
)

// Option is one choice of a KindSelect field.
type Option struct {
	Value    string
	LabelKey string // translation key
}

// Field is one setting of a source.
type Field struct {
	Key      string // short name, unique in its source, [a-z0-9]+
	Kind     Kind
	LabelKey string // translation key
	HelpKey  string // optional translation key for a hint under the field
	Options  []Option
	Required bool
	Default  string

	// LegacyKey is the existing `glucava config` key that already stores this
	// field (dexcom_region, dexcom_username). New sources leave it empty and
	// get the key source_<id>_<key>.
	LegacyKey string
	// SecretName is the vault name of a KindPassword field that existed before
	// sources were generic. Empty gives source_<id>_<key>.
	SecretName string
}

// Secret reports whether the field is stored in the vault.
func (f Field) Secret() bool { return f.Kind == KindPassword }

// Values gives a source's settings to its Build function. A secret field
// returns its decrypted value.
type Values interface {
	Get(field string) string
}

// Descriptor describes one live glucose source.
type Descriptor struct {
	ID       string // stored in settings and used as the key readings are saved under, [a-z0-9]+
	LabelKey string
	IntroKey string // translation key for a short explanation shown in Settings
	// Unverified marks a source that was written from the service's public
	// behaviour but never run against a real account. The UI says so.
	Unverified bool
	Fields     []Field
	// Build creates the Source. It is called again whenever a value changes, so
	// it must not read settings itself. Missing required fields are rejected
	// before it is called.
	Build func(v Values) (Source, error)
}

var registry []Descriptor

// Register adds a source. It panics on a bad or duplicate description, which is
// a programming error caught by the first test run.
func Register(d Descriptor) {
	if d.ID == "" || d.Build == nil || d.LabelKey == "" {
		panic("glucose: source needs an id, a label and a Build function")
	}
	if _, ok := Lookup(d.ID); ok {
		panic("glucose: source registered twice: " + d.ID)
	}
	seen := map[string]bool{}
	for _, f := range d.Fields {
		if f.Key == "" || seen[f.Key] || f.LabelKey == "" {
			panic(fmt.Sprintf("glucose: source %s has a bad or duplicate field %q", d.ID, f.Key))
		}
		if f.Kind == KindSelect && len(f.Options) == 0 {
			panic(fmt.Sprintf("glucose: source %s select field %s has no options", d.ID, f.Key))
		}
		seen[f.Key] = true
	}
	registry = append(registry, d)
}

// All returns every registered source in registration order (the order the UI lists them).
func All() []Descriptor { return slices.Clone(registry) }

// Lookup finds a source by id.
func Lookup(id string) (Descriptor, bool) {
	for _, d := range registry {
		if d.ID == id {
			return d, true
		}
	}
	return Descriptor{}, false
}

// IDs lists the registered source ids.
func IDs() []string {
	out := make([]string, len(registry))
	for i, d := range registry {
		out[i] = d.ID
	}
	return out
}

// SettingKey is the `glucava config` key of a non-secret field.
func SettingKey(id string, f Field) string {
	if f.LegacyKey != "" {
		return f.LegacyKey
	}
	return "source_" + id + "_" + f.Key
}

// SecretName is the vault name of a secret field.
func SecretName(id string, f Field) string {
	if f.SecretName != "" {
		return f.SecretName
	}
	return "source_" + id + "_" + f.Key
}

// SecretNames lists the vault names of every source's secret fields.
func SecretNames() []string {
	var out []string
	for _, d := range registry {
		for _, f := range d.Fields {
			if f.Secret() {
				out = append(out, SecretName(d.ID, f))
			}
		}
	}
	return out
}

// Problem describes why settings were rejected, as a translation key with arguments.
type Problem struct {
	Key  string
	Args []any
}

// Validate checks the non-secret settings of source id; get returns a field's
// current value. Required fields are not checked here: a half-filled source is
// fine until it is the active one and used (see MissingRequired).
func Validate(id string, get func(Field) string) (Problem, bool) {
	d, ok := Lookup(id)
	if !ok {
		return Problem{Key: i18n.Key("validate.source"), Args: []any{"source", id, "known", strings.Join(IDs(), ", ")}}, false
	}
	for _, f := range d.Fields {
		v := get(f)
		if v == "" || f.Secret() {
			continue
		}
		switch f.Kind {
		case KindSelect:
			if !slices.ContainsFunc(f.Options, func(o Option) bool { return o.Value == v }) {
				return Problem{Key: i18n.Key("validate.source_option"), Args: []any{"field", f.Key}}, false
			}
		case KindURL:
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return Problem{Key: i18n.Key("validate.source_url"), Args: []any{"field", f.Key}}, false
			}
		}
		if len(v) > 300 {
			return Problem{Key: i18n.Key("validate.source_long"), Args: []any{"field", f.Key}}, false
		}
	}
	return Problem{}, true
}

// MissingRequired returns the first required field of d for which get is empty.
func MissingRequired(d Descriptor, get func(Field) string) (Field, bool) {
	for _, f := range d.Fields {
		if f.Required && get(f) == "" {
			return f, true
		}
	}
	return Field{}, false
}

// Named is implemented by a Source that knows under which name its readings are
// stored. The processor and the ingest loop ask it, so switching the source in
// Settings needs no restart.
type Named interface {
	Name() string
}

// NameOf returns the name readings from src are stored under: its own when it
// is a Named source, otherwise fallback.
func NameOf(src Source, fallback string) string {
	if n, ok := src.(Named); ok {
		return n.Name()
	}
	return fallback
}
