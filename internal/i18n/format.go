package i18n

import (
	"strconv"
	"strings"
	"time"
)

// Locale-aware formatting. The patterns and names live in the locale files
// under "format.*" so a translator can set them without touching code.

// Weekday is the name of d (Sunday is 0), short ("Mon") or long ("Monday").
func (t *Translator) Weekday(d time.Weekday, long bool) string {
	if long {
		return t.T("format.weekday.long." + strconv.Itoa(int(d)))
	}
	return t.T("format.weekday.short." + strconv.Itoa(int(d)))
}

// Month is the name of m, short ("Jan") or long ("January").
func (t *Translator) Month(m time.Month, long bool) string {
	if long {
		return t.T("format.month.long." + strconv.Itoa(int(m)))
	}
	return t.T("format.month.short." + strconv.Itoa(int(m)))
}

// Time formats the clock time of tm using the locale's layout (a Go
// reference-time layout, "15:04" by default: 24-hour).
func (t *Translator) Time(tm time.Time) string {
	return tm.Format(t.T("format.time"))
}

// Date formats the day of tm as "2 Jan" (withYear false) or "2 Jan 2006".
func (t *Translator) Date(tm time.Time, withYear bool) string {
	key := "format.date"
	if withYear {
		key = "format.date.year"
	}
	return t.T(key, "day", tm.Day(), "month", t.Month(tm.Month(), false), "year", strconv.Itoa(tm.Year()))
}

// When renders tm in loc for a table: with the weekday and without the year
// in the current year of now ("Wed 30 Sep, 07:00"), with the year otherwise.
// A zero time is "-".
func (t *Translator) When(tm time.Time, loc *time.Location, now time.Time) string {
	if tm.IsZero() {
		return "-"
	}
	tm = tm.In(loc)
	args := []any{
		"weekday", t.Weekday(tm.Weekday(), false), "day", strconv.Itoa(tm.Day()),
		"month", t.Month(tm.Month(), false), "year", strconv.Itoa(tm.Year()), "time", t.Time(tm),
	}
	if tm.Year() == now.In(loc).Year() {
		return t.T("format.when", args...)
	}
	return t.T("format.when.year", args...)
}

// Num formats v with the locale's decimal and thousands separators.
// decimals < 0 prints the shortest exact form.
func (t *Translator) Num(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(s, ".")
	if sep := t.thousands(); sep != "" && len(whole) > 3 {
		var b strings.Builder
		for i, r := range whole {
			if i > 0 && (len(whole)-i)%3 == 0 {
				b.WriteString(sep)
			}
			b.WriteRune(r)
		}
		whole = b.String()
	}
	if hasFrac {
		whole += t.decimal() + frac
	}
	if neg {
		return "-" + whole
	}
	return whole
}

// Int formats n with the locale's thousands separator.
func (t *Translator) Int(n int) string { return t.Num(float64(n), 0) }

func (t *Translator) decimal() string {
	if s, ok := t.lookup("format.decimal"); ok {
		return s
	}
	return "."
}

func (t *Translator) thousands() string {
	if s, ok := t.lookup("format.thousands"); ok {
		return s
	}
	return ""
}

// List joins items as prose: "a", "a and b", "a, b and c". The wording comes
// from format.list.pair and format.list.last / format.list.sep.
func (t *Translator) List(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return t.T("format.list.pair", "a", items[0], "b", items[1])
	}
	sep := t.T("format.list.sep")
	return t.T("format.list.last", "rest", strings.Join(items[:len(items)-1], sep), "last", items[len(items)-1])
}
