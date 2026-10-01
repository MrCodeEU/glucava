package i18n

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testBundle(t *testing.T) *Bundle {
	t.Helper()
	meta := func(name, fb string) string {
		return `"_meta":{"name":"` + name + `","native_name":"` + name + `","fallback":"` + fb + `"}`
	}
	fsys := fstest.MapFS{
		"l/en.json": {Data: []byte(`{` + meta("English", "") + `,
			"hello":"Hello {name}","only.en":"only english","items.one":"{n} item","items.other":"{n} items",
			"format.decimal":".","format.thousands":",","format.time":"15:04",
			"format.weekday.short.3":"Wed","format.month.short.9":"Sep","format.when":"{weekday} {day} {month}, {time}","format.when.year":"{day} {month} {year}, {time}",
			"format.list.pair":"{a} and {b}","format.list.last":"{rest} and {last}","format.list.sep":", "}`)},
		"l/de.json": {Data: []byte(`{` + meta("Deutsch", "en") + `,"hello":"Hallo {name}","items.one":"{n} Eintrag","items.other":"{n} Einträge",
			"format.decimal":",","format.thousands":".","format.weekday.short.3":"Mi","format.list.pair":"{a} und {b}","format.when":"{weekday}, {day}. {month}, {time}"}`)},
		"l/de-AT.json": {Data: []byte(`{` + meta("Deutsch (AT)", "de") + `,"hello":"Servus {name}"}`)},
		"l/ru.json":    {Data: []byte(`{` + meta("Русский", "en") + `,"items.one":"{n} запись","items.few":"{n} записи","items.many":"{n} записей","items.other":"{n} записи"}`)},
	}
	b, err := Load(fsys, "l")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEmbeddedLocalesLoad(t *testing.T) {
	b := Default()
	for _, tag := range []string{"en", "de", "de-AT"} {
		if !b.Has(tag) {
			t.Errorf("embedded locale %s missing", tag)
		}
	}
	if got := English().T("nav.activities"); got != "Activities" {
		t.Errorf("English nav.activities = %q", got)
	}
	if got := b.For("de").T("nav.activities"); got != "Aktivitäten" {
		t.Errorf("German nav.activities = %q", got)
	}
}

func TestTAndPlaceholders(t *testing.T) {
	b := testBundle(t)
	de := b.For("de")
	if got := de.T("hello", "name", "Ada"); got != "Hallo Ada" {
		t.Errorf("got %q", got)
	}
	if got := de.T("hello"); got != "Hallo {name}" { // unfilled placeholder stays visible
		t.Errorf("got %q", got)
	}
	if got := de.T("hello", "name", 7); got != "Hallo 7" {
		t.Errorf("int arg: %q", got)
	}
}

func TestFallbackChain(t *testing.T) {
	b := testBundle(t)
	at := b.For("de-AT")
	if got := at.T("hello", "name", "X"); got != "Servus X" {
		t.Errorf("own message: %q", got)
	}
	if got := at.T("items.other", "n", 2); got != "2 Einträge" { // de-AT -> de
		t.Errorf("fallback to de: %q", got)
	}
	if got := at.T("only.en"); got != "only english" { // -> en
		t.Errorf("fallback to en: %q", got)
	}
}

func TestMissingKeyReturnsKeyAndReportsOnce(t *testing.T) {
	b := testBundle(t)
	var seen []string
	old := OnMissing
	OnMissing = func(tag, key string) { seen = append(seen, tag+":"+key) }
	defer func() { OnMissing = old }()
	de := b.For("de")
	for i := 0; i < 3; i++ {
		if got := de.T("nope.unique.key"); got != "nope.unique.key" {
			t.Fatalf("got %q", got)
		}
	}
	if len(seen) != 1 || seen[0] != "de:nope.unique.key" {
		t.Errorf("OnMissing calls = %v", seen)
	}
}

func TestPlurals(t *testing.T) {
	b := testBundle(t)
	cases := []struct {
		tag  string
		n    int
		want string
	}{
		{"en", 1, "1 item"}, {"en", 0, "0 items"}, {"en", 1234, "1,234 items"},
		{"de", 1, "1 Eintrag"}, {"de", 2, "2 Einträge"},
		{"ru", 1, "1 запись"}, {"ru", 2, "2 записи"}, {"ru", 5, "5 записей"}, {"ru", 11, "11 записей"}, {"ru", 21, "21 запись"}, {"ru", 22, "22 записи"},
	}
	for _, c := range cases {
		if got := b.For(c.tag).Tn("items", c.n); got != c.want {
			t.Errorf("%s n=%d: got %q want %q", c.tag, c.n, got, c.want)
		}
	}
}

func TestPluralRules(t *testing.T) {
	cases := []struct {
		tag  string
		n    int
		want string
	}{
		{"en", 1, "one"}, {"en", 2, "other"}, {"fr", 0, "one"}, {"fr", 2, "other"},
		{"pl", 1, "one"}, {"pl", 3, "few"}, {"pl", 12, "many"}, {"pl", 22, "few"},
		{"cs", 3, "few"}, {"cs", 5, "other"}, {"ja", 1, "other"}, {"pt-BR", 0, "one"},
	}
	for _, c := range cases {
		if got := pluralFor(c.tag)(c.n); got != c.want {
			t.Errorf("%s %d: got %s want %s", c.tag, c.n, got, c.want)
		}
	}
}

func TestMatch(t *testing.T) {
	b := testBundle(t)
	cases := []struct {
		accept, configured, want string
	}{
		{"", "", "en"},
		{"de", "auto", "de"},
		{"de-AT,de;q=0.9,en;q=0.8", "", "de-AT"},
		{"de-CH", "", "de"}, // base language
		{"fr-FR,fr;q=0.9,de;q=0.5", "", "de"},
		{"en-US,en;q=0.9", "", "en"},
		{"*;q=0.5", "", "en"},
		{"de", "en", "en"}, // explicit choice wins over the browser
		{"de", "xx", "de"}, // unknown configured tag defers to the browser
		{"ru;q=0,de;q=0.1", "", "de"},
		{"DE-at", "", "de-AT"}, // tags are case-insensitive
	}
	for _, c := range cases {
		if got := b.Match(c.accept, c.configured).Tag(); got != c.want {
			t.Errorf("Match(%q,%q) = %s, want %s", c.accept, c.configured, got, c.want)
		}
	}
}

func TestFormatting(t *testing.T) {
	b := testBundle(t)
	en, de := b.For("en"), b.For("de")
	if got := en.Num(1234567.891, 2); got != "1,234,567.89" {
		t.Errorf("en num: %q", got)
	}
	if got := de.Num(1234567.891, 2); got != "1.234.567,89" {
		t.Errorf("de num: %q", got)
	}
	if got := de.Num(-5.5, 1); got != "-5,5" {
		t.Errorf("negative: %q", got)
	}
	if got := en.Num(12.5, -1); got != "12.5" {
		t.Errorf("shortest: %q", got)
	}
	loc := time.UTC
	tm := time.Date(2026, 9, 30, 7, 5, 0, 0, loc) // a Wednesday
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
	if got := en.When(tm, loc, now); got != "Wed 30 Sep, 07:05" {
		t.Errorf("en when: %q", got)
	}
	if got := de.When(tm, loc, now); got != "Mi, 30. Sep, 07:05" {
		t.Errorf("de when: %q", got)
	}
	if got := en.When(tm, loc, now.AddDate(1, 0, 0)); got != "30 Sep 2026, 07:05" {
		t.Errorf("other year: %q", got)
	}
	if got := en.When(time.Time{}, loc, now); got != "-" {
		t.Errorf("zero time: %q", got)
	}
	if got := en.List([]string{"a", "b", "c"}); got != "a, b and c" {
		t.Errorf("list: %q", got)
	}
	if got := de.List([]string{"a", "b"}); got != "a und b" {
		t.Errorf("list two: %q", got)
	}
}

func TestParseLocaleErrors(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":      `nope`,
		"non-string":    `{"_meta":{"name":"x","native_name":"x"},"a":1}`,
		"missing names": `{"a":"b"}`,
	} {
		if _, err := ParseLocale("xx", []byte(raw)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestLoadRequiresEnglish(t *testing.T) {
	_, err := Load(fstest.MapFS{"l/de.json": {Data: []byte(`{"_meta":{"name":"a","native_name":"b"}}`)}}, "l")
	if err == nil || !strings.Contains(err.Error(), "base language") {
		t.Errorf("err = %v", err)
	}
}

func TestLocalesListing(t *testing.T) {
	infos := testBundle(t).Locales()
	if len(infos) != 4 || infos[0].Tag != "de" || infos[1].Tag != "de-AT" {
		t.Errorf("Locales() = %+v", infos)
	}
}

func BenchmarkTNoArgs(b *testing.B) {
	tr := English()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = tr.T("nav.activities")
	}
}
