package i18n

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Translator looks messages up for one locale. It is immutable and safe for
// concurrent use; hold one per request.
type Translator struct {
	b     *Bundle
	loc   *Locale
	chain []*Locale
}

// Tag is the locale tag the translator was made for, e.g. "de".
func (t *Translator) Tag() string { return t.loc.Tag }

// Lang is the tag for the HTML lang attribute.
func (t *Translator) Lang() string { return t.loc.Tag }

// lookup finds key along the fallback chain.
func (t *Translator) lookup(key string) (string, bool) {
	for _, l := range t.chain {
		if s, ok := l.msgs[key]; ok {
			return s, true
		}
	}
	return "", false
}

// Has reports whether key resolves to a message (in any locale of the chain).
func (t *Translator) Has(key string) bool {
	_, ok := t.lookup(key)
	return ok
}

// T returns the message for key. args are alternating placeholder names and
// values: T("greeting", "name", "Ada") fills {name}. A key found nowhere is
// returned as it is (and reported once through OnMissing).
func (t *Translator) T(key string, args ...any) string {
	s, ok := t.lookup(key)
	if !ok {
		reportMissing(t.loc.Tag, key)
		return key
	}
	return t.expand(s, args)
}

// Tn returns the plural form of key for n: the locale's rule picks a suffix
// (key.one, key.few, ...) with key.other as the fallback. {n} is filled in
// automatically, formatted for the locale.
func (t *Translator) Tn(key string, n int, args ...any) string {
	cat := t.loc.plur(n)
	s, ok := t.lookup(key + "." + cat)
	if !ok {
		s, ok = t.lookup(key + ".other")
	}
	if !ok {
		reportMissing(t.loc.Tag, key+".other")
		return key
	}
	all := make([]any, 0, len(args)+2)
	all = append(all, "n", n)
	all = append(all, args...)
	return t.expand(s, all)
}

// expand fills {name} placeholders. A text without braces is returned as it is.
func (t *Translator) expand(s string, args []any) string {
	if !strings.Contains(s, "{") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); {
		if s[i] != '{' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			b.WriteString(s[i:])
			break
		}
		name := s[i+1 : i+end]
		if v, ok := t.arg(args, name); ok {
			b.WriteString(v)
		} else {
			b.WriteString(s[i : i+end+1]) // unknown placeholder stays visible
		}
		i += end + 1
	}
	return b.String()
}

func (t *Translator) arg(args []any, name string) (string, bool) {
	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok && k == name {
			return t.value(args[i+1]), true
		}
	}
	return "", false
}

func (t *Translator) value(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return t.Int(x)
	case int64:
		return t.Int(int(x))
	case float64:
		return t.Num(x, -1)
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprint(v)
}

// ----------------------------------------------------------------- matching

// Match chooses a translator. configured is the stored language setting: a
// loaded tag wins; "" or Auto (or an unknown tag) defers to the browser's
// Accept-Language header; English is the last resort.
func (b *Bundle) Match(acceptLanguage, configured string) *Translator {
	if configured != "" && configured != Auto {
		if t := b.trs[strings.ToLower(configured)]; t != nil {
			return t
		}
	}
	for _, tag := range parseAcceptLanguage(acceptLanguage) {
		if t := b.trs[strings.ToLower(tag)]; t != nil {
			return t
		}
		if i := strings.IndexAny(tag, "-_"); i > 0 { // de-AT -> de
			if t := b.trs[strings.ToLower(tag[:i])]; t != nil {
				return t
			}
		}
	}
	return b.trs[Base]
}

// parseAcceptLanguage returns the tags of an Accept-Language header by
// descending quality, ignoring "*" and q=0.
func parseAcceptLanguage(h string) []string {
	type cand struct {
		tag string
		q   float64
		ord int
	}
	var cs []cand
	for i, part := range strings.Split(h, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, q := part, 1.0
		if semi := strings.IndexByte(part, ';'); semi >= 0 {
			tag = strings.TrimSpace(part[:semi])
			for _, p := range strings.Split(part[semi+1:], ";") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "q=") {
					if v, err := strconv.ParseFloat(p[2:], 64); err == nil {
						q = v
					}
				}
			}
		}
		if tag == "*" || q <= 0 {
			continue
		}
		cs = append(cs, cand{tag, q, i})
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].q > cs[j].q })
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.tag
	}
	return out
}
