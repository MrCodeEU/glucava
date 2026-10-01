// Package i18n is glucava's translation layer: locale files are plain JSON
// embedded in the binary and discovered at start-up, so adding a language is
// a data change (see docs/TRANSLATING.md), not a code change.
//
// A locale file is locales/<tag>.json: a flat object of message key to text,
// plus one "_meta" object. Texts use {name} placeholders; plurals use the key
// suffixes .zero .one .two .few .many .other (CLDR categories). Lookups fall
// back from the chosen locale through its _meta.fallback to English and, if
// the key exists nowhere, return the key itself so a gap is visible and never
// crashes a page.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed locales/*.json
var embedded embed.FS

// Base is the language every other one falls back to and the one that must be
// complete: the key set of Base defines which keys exist.
const Base = "en"

// Auto is the configured-language value meaning "follow the browser".
const Auto = "auto"

// Meta is the "_meta" object of a locale file.
type Meta struct {
	Name        string   `json:"name"`        // English name, e.g. "German"
	NativeName  string   `json:"native_name"` // the language's own name, e.g. "Deutsch"
	Translators []string `json:"translators"` // credit, free text
	Fallback    string   `json:"fallback"`    // tag tried before English, e.g. "pt" for "pt-BR"
}

// Locale is one loaded language.
type Locale struct {
	Tag  string
	Meta Meta
	msgs map[string]string
	plur pluralFunc
}

// Keys returns the locale's message keys, sorted (the "_meta" entry is not one).
func (l *Locale) Keys() []string {
	out := make([]string, 0, len(l.msgs))
	for k := range l.msgs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Message returns the raw text of key in this locale only (no fallback).
func (l *Locale) Message(key string) (string, bool) {
	s, ok := l.msgs[key]
	return s, ok
}

// Info describes a locale for a language picker.
type Info struct{ Tag, Name, NativeName string }

// Bundle is the set of loaded locales.
type Bundle struct {
	locales map[string]*Locale // by lower-case tag
	tags    []string           // original-case tags, sorted
	trs     map[string]*Translator
}

var (
	defaultOnce   sync.Once
	defaultBundle *Bundle
)

// Default returns the bundle built from the embedded locale files. It panics
// if they are malformed, which the package tests rule out.
func Default() *Bundle {
	defaultOnce.Do(func() {
		b, err := Load(embedded, "locales")
		if err != nil {
			panic("i18n: embedded locales: " + err.Error())
		}
		defaultBundle = b
	})
	return defaultBundle
}

// English returns the English translator of the default bundle, for code that
// has no request (tests, logs, unconverted helpers).
func English() *Translator { return Default().For(Base) }

// Key marks a string as a translation key without translating it. Use it where
// a key is stored in a table and translated later, so tools/i18n and the
// key-check test can still see and verify it.
func Key(k string) string { return k }

// Load reads every *.json file in dir of fsys as a locale. English must exist.
func Load(fsys fs.FS, dir string) (*Bundle, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	b := &Bundle{locales: map[string]*Locale{}, trs: map[string]*Translator{}}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".json" {
			continue
		}
		tag := strings.TrimSuffix(e.Name(), ".json")
		raw, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		loc, err := parseLocale(tag, raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		b.locales[strings.ToLower(tag)] = loc
		b.tags = append(b.tags, tag)
	}
	sort.Strings(b.tags)
	if b.locales[Base] == nil {
		return nil, fmt.Errorf("no %s.json: the base language is required", Base)
	}
	for k, loc := range b.locales {
		b.trs[k] = &Translator{b: b, loc: loc, chain: b.chain(loc)}
	}
	return b, nil
}

// ParseLocale decodes one locale file; exported for tools/i18n and tests.
func ParseLocale(tag string, raw []byte) (*Locale, error) { return parseLocale(tag, raw) }

func parseLocale(tag string, raw []byte) (*Locale, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	loc := &Locale{Tag: tag, msgs: make(map[string]string, len(top)), plur: pluralFor(tag)}
	for k, v := range top {
		if k == "_meta" {
			if err := json.Unmarshal(v, &loc.Meta); err != nil {
				return nil, fmt.Errorf("_meta: %w", err)
			}
			continue
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, fmt.Errorf("key %q: the value must be a string", k)
		}
		loc.msgs[k] = s
	}
	if loc.Meta.Name == "" || loc.Meta.NativeName == "" {
		return nil, fmt.Errorf("_meta needs name and native_name")
	}
	return loc, nil
}

// chain is the lookup order for loc: itself, its fallbacks, then English.
func (b *Bundle) chain(loc *Locale) []*Locale {
	out := []*Locale{loc}
	seen := map[string]bool{strings.ToLower(loc.Tag): true}
	for cur := loc; cur.Meta.Fallback != ""; {
		next := b.locales[strings.ToLower(cur.Meta.Fallback)]
		if next == nil || seen[strings.ToLower(next.Tag)] {
			break
		}
		seen[strings.ToLower(next.Tag)] = true
		out = append(out, next)
		cur = next
	}
	if en := b.locales[Base]; !seen[Base] && en != nil {
		out = append(out, en)
	}
	return out
}

// Locales lists the loaded languages, sorted by tag.
func (b *Bundle) Locales() []Info {
	out := make([]Info, 0, len(b.tags))
	for _, t := range b.tags {
		l := b.locales[strings.ToLower(t)]
		out = append(out, Info{Tag: l.Tag, Name: l.Meta.Name, NativeName: l.Meta.NativeName})
	}
	return out
}

// Locale returns the loaded locale for tag (case-insensitive), or nil.
func (b *Bundle) Locale(tag string) *Locale { return b.locales[strings.ToLower(tag)] }

// Has reports whether tag is a loaded locale.
func (b *Bundle) Has(tag string) bool { return b.Locale(tag) != nil }

// For returns the translator for tag, or the English one if tag is unknown.
func (b *Bundle) For(tag string) *Translator {
	if t := b.trs[strings.ToLower(tag)]; t != nil {
		return t
	}
	return b.trs[Base]
}

// OnMissing is called once per (locale, key) when a key exists in no locale of
// the lookup chain. The default logs a warning; tests replace it.
var OnMissing = func(tag, key string) {
	slog.Warn("i18n: missing translation key", "locale", tag, "key", key)
}

var missingSeen sync.Map

func reportMissing(tag, key string) {
	if _, dup := missingSeen.LoadOrStore(tag+"\x00"+key, true); !dup {
		OnMissing(tag, key)
	}
}
