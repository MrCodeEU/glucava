package web

import (
	"net/url"
	"time"

	"github.com/MrCodeEU/glucava/internal/store"
)

// statsRangeLabels names the range presets (store.OverviewRanges), which are
// also the ?range= values statsPage accepts.
var statsRangeLabels = map[string]string{
	"7d": "7 days", "14d": "14 days", "30d": "30 days", "90d": "90 days", "all": "All time",
}

// presetDays is the length of each rolling preset in days; "all" has none.
var presetDays = map[string]int{"7d": 7, "14d": 14, "30d": 30, "90d": 90}

// maxCustomSpanDays bounds a custom range, roughly ten years.
const maxCustomSpanDays = 3660

// dateLayout is the value of an <input type="date"> and of ?from= / ?to=.
const dateLayout = "2006-01-02"

// statsRange is the window the Overview shows: a preset or a custom
// from-to, plus the equally long window before it when comparing.
type statsRange struct {
	Key      string    // a preset key, or "custom"
	From, To time.Time // [From, To]
	// FromStr and ToStr are the ?from= / ?to= values of a custom range, so a
	// link can keep the range while changing something else.
	FromStr, ToStr string
	Compare        bool      // compare with the previous period
	PrevFrom       time.Time // the previous window, [PrevFrom, PrevTo]; set when Compare
	PrevTo         time.Time
	Note           string // why the request fell back to the default, "" when it did not
}

// parseStatsRange reads the Overview's query: ?range=<preset>, or a custom
// ?from=&to= (dates, in loc, both ends inclusive), plus ?compare=prev. An
// empty or unknown range uses def (the configured default preset); a bad
// custom range falls back to def and says why in Note. "all" has nothing
// before it, so it never compares.
func parseStatsRange(q url.Values, def string, now time.Time, loc *time.Location) statsRange {
	if _, ok := statsRangeLabels[def]; !ok {
		def = store.DefaultOverviewRange
	}
	r := statsRange{}
	if fs, ts := q.Get("from"), q.Get("to"); fs != "" || ts != "" {
		from, to, note := parseCustomRange(fs, ts, now, loc)
		if note == "" {
			r.Key, r.From, r.To, r.FromStr, r.ToStr = "custom", from, to, fs, ts
		} else {
			r.Note = note
		}
	}
	if r.Key == "" {
		key := q.Get("range")
		if _, ok := statsRangeLabels[key]; !ok {
			key = def
		}
		r.Key, r.To = key, now
		if key == "all" {
			r.From = now.AddDate(-10, 0, 0) // far enough back to include everything real
		} else {
			r.From = now.AddDate(0, 0, -presetDays[key])
		}
	}
	if q.Get("compare") == "prev" && r.Key != "all" {
		r.Compare = true
		r.PrevTo = r.From
		r.PrevFrom = r.From.Add(-r.To.Sub(r.From))
	}
	return r
}

// parseCustomRange validates ?from= and ?to=. The end date is inclusive: the
// window runs to the end of that day, or to now when that day is not over.
func parseCustomRange(fromStr, toStr string, now time.Time, loc *time.Location) (from, to time.Time, note string) {
	if fromStr == "" || toStr == "" {
		return from, to, "Pick both a start and an end date."
	}
	f, err1 := time.ParseInLocation(dateLayout, fromStr, loc)
	t, err2 := time.ParseInLocation(dateLayout, toStr, loc)
	if err1 != nil || err2 != nil {
		return from, to, "Dates must look like 2026-09-30."
	}
	end := t.AddDate(0, 0, 1)
	switch {
	case t.Before(f):
		return from, to, "The end date is before the start date."
	case !f.Before(now):
		return from, to, "The start date is in the future."
	case end.Sub(f) > maxCustomSpanDays*24*time.Hour:
		return from, to, "A custom range can span at most ten years."
	}
	if end.After(now) {
		end = now
	}
	return f, end, ""
}

// href is the Overview URL for this range with compare set as asked; other
// parameters are not carried.
func (r statsRange) href(compare bool) string {
	q := url.Values{}
	if r.Key == "custom" {
		q.Set("from", r.FromStr)
		q.Set("to", r.ToStr)
	} else {
		q.Set("range", r.Key)
	}
	if compare && r.Key != "all" {
		q.Set("compare", "prev")
	}
	return "/stats?" + q.Encode()
}

// presetHref links to a preset, keeping the compare choice.
func (r statsRange) presetHref(key string) string {
	q := url.Values{"range": {key}}
	if r.Compare && key != "all" {
		q.Set("compare", "prev")
	}
	return "/stats?" + q.Encode()
}

// Label describes the window, e.g. "16 Sep – 30 Sep", for card subtitles.
func (r statsRange) Label(loc *time.Location) string {
	if r.Key == "all" {
		return "All time"
	}
	return r.From.In(loc).Format("2 Jan") + " – " + r.To.In(loc).Format("2 Jan")
}
