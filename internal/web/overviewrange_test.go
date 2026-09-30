package web

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseStatsRangePresets(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, loc)
	cases := []struct {
		query, def, key string
		days            int
	}{
		{"", "", "30d", 30},
		{"range=7d", "30d", "7d", 7},
		{"range=14d", "30d", "14d", 14},
		{"range=90d", "30d", "90d", 90},
		{"range=bogus", "14d", "14d", 14}, // unknown falls back to the configured default
		{"", "90d", "90d", 90},
		{"", "nonsense", "30d", 30}, // an invalid default falls back to 30d
	}
	for _, c := range cases {
		q, _ := url.ParseQuery(c.query)
		r := parseStatsRange(q, c.def, now, loc)
		if r.Key != c.key || !r.To.Equal(now) || !r.From.Equal(now.AddDate(0, 0, -c.days)) {
			t.Errorf("%q/%q: got %s %v..%v", c.query, c.def, r.Key, r.From, r.To)
		}
		if r.Compare || r.Note != "" {
			t.Errorf("%q: unexpected compare/note %+v", c.query, r)
		}
	}
	all := parseStatsRange(url.Values{"range": {"all"}}, "", now, loc)
	if all.Key != "all" || all.From.After(now.AddDate(-9, 0, 0)) {
		t.Errorf("all = %+v", all)
	}
}

func TestParseStatsRangeCustom(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Vienna")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, loc)
	q := url.Values{"from": {"2026-09-01"}, "to": {"2026-09-10"}}
	r := parseStatsRange(q, "", now, loc)
	if r.Key != "custom" || r.Note != "" {
		t.Fatalf("custom = %+v", r)
	}
	if !r.From.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, loc)) || !r.To.Equal(time.Date(2026, 9, 11, 0, 0, 0, 0, loc)) {
		t.Errorf("bounds %v..%v (the end date is inclusive: through the end of that day)", r.From, r.To)
	}
	// An end date that is today runs to now, not to the end of the day.
	r = parseStatsRange(url.Values{"from": {"2026-09-20"}, "to": {"2026-09-30"}}, "", now, loc)
	if !r.To.Equal(now) {
		t.Errorf("to = %v, want now", r.To)
	}

	bad := map[string]url.Values{
		"before":   {"from": {"2026-09-10"}, "to": {"2026-09-01"}},
		"missing":  {"from": {"2026-09-10"}},
		"garbage":  {"from": {"x"}, "to": {"y"}},
		"future":   {"from": {"2027-01-01"}, "to": {"2027-02-01"}},
		"too long": {"from": {"2010-01-01"}, "to": {"2026-09-01"}},
	}
	for name, q := range bad {
		r := parseStatsRange(q, "14d", now, loc)
		if r.Key != "14d" || r.Note == "" {
			t.Errorf("%s: %+v, want the default preset with a note", name, r)
		}
	}
}

func TestParseStatsRangeCompare(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, loc)
	r := parseStatsRange(url.Values{"range": {"7d"}, "compare": {"prev"}}, "", now, loc)
	if !r.Compare || !r.PrevTo.Equal(r.From) || r.PrevTo.Sub(r.PrevFrom) != r.To.Sub(r.From) {
		t.Errorf("compare window = %v..%v for %v..%v", r.PrevFrom, r.PrevTo, r.From, r.To)
	}
	// "all" has no previous period.
	if a := parseStatsRange(url.Values{"range": {"all"}, "compare": {"prev"}}, "", now, loc); a.Compare {
		t.Error("all must not compare")
	}
	c := parseStatsRange(url.Values{"from": {"2026-09-01"}, "to": {"2026-09-10"}, "compare": {"prev"}}, "", now, loc)
	if !c.Compare || !c.PrevTo.Equal(c.From) {
		t.Errorf("custom compare = %+v", c)
	}
}

func TestStatsRangeHrefs(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	r := parseStatsRange(url.Values{"range": {"30d"}, "compare": {"prev"}}, "", now, time.UTC)
	if got := r.presetHref("7d"); got != "/stats?compare=prev&range=7d" {
		t.Errorf("presetHref = %s", got)
	}
	if got := r.presetHref("all"); strings.Contains(got, "compare") {
		t.Errorf("all keeps compare: %s", got)
	}
	if got := r.href(false); got != "/stats?range=30d" {
		t.Errorf("href(false) = %s", got)
	}
	c := parseStatsRange(url.Values{"from": {"2026-09-01"}, "to": {"2026-09-10"}}, "", now, time.UTC)
	if got := c.href(true); got != "/stats?compare=prev&from=2026-09-01&to=2026-09-10" {
		t.Errorf("custom href = %s", got)
	}
}
