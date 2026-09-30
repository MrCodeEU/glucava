package i18n

import "strings"

// pluralFunc returns the CLDR plural category of n: "zero", "one", "two",
// "few", "many" or "other".
type pluralFunc func(n int) string

// pluralFor picks the rule for a locale tag by its base language. This small
// table covers the common languages; a language not listed uses the English
// rule (one / other). To add a rule, add the language code to a case below
// (or a new rule) — see docs/TRANSLATING.md.
func pluralFor(tag string) pluralFunc {
	base := strings.ToLower(tag)
	if i := strings.IndexAny(base, "-_"); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "fr", "pt", "hi":
		return func(n int) string {
			if n == 0 || n == 1 {
				return "one"
			}
			return "other"
		}
	case "ru", "uk", "be":
		return func(n int) string {
			m10, m100 := abs(n)%10, abs(n)%100
			switch {
			case m10 == 1 && m100 != 11:
				return "one"
			case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
				return "few"
			default:
				return "many"
			}
		}
	case "pl":
		return func(n int) string {
			m10, m100 := abs(n)%10, abs(n)%100
			switch {
			case n == 1:
				return "one"
			case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
				return "few"
			default:
				return "many"
			}
		}
	case "cs", "sk":
		return func(n int) string {
			switch {
			case n == 1:
				return "one"
			case n >= 2 && n <= 4:
				return "few"
			default:
				return "other"
			}
		}
	case "ja", "zh", "ko", "vi", "th", "id", "ms":
		return func(int) string { return "other" }
	}
	return func(n int) string { // en, de, nl, es, it, sv, da, nb, fi, ...
		if n == 1 {
			return "one"
		}
		return "other"
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
