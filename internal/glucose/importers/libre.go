package importers

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

func init() {
	Register("libre", libreImporter{})
}

// libreImporter reads a LibreView "Glucose Data" CSV export (Abbott's
// account export, download from libreview.com: Reports > Export).
//
// UNVERIFIED: written from published community documentation of the export
// format, not tested against a real file (the maintainer does not use a
// Libre device). Report a mismatch with a redacted sample of your export's
// header row; this parser is defensive by design (unknown/unparsable rows
// are counted and skipped, not fatal) specifically so a format drift shows
// up as "N rows skipped" rather than a hard failure.
//
// The export has a title row before the real header, and column names and
// number/date formats vary by account locale, so this looks up columns by
// (case-insensitive) name rather than fixed positions, and accepts either a
// "... mg/dL" or "... mmol/L" glucose column, and a few common timestamp
// layouts.
type libreImporter struct{}

var libreTimeLayouts = []string{
	"02-01-2006 15:04",
	"01-02-2006 15:04",
	"2006-01-02 15:04",
	"02/01/2006 15:04",
	"01/02/2006 15:04",
	"2006-01-02T15:04:05",
}

func (libreImporter) Parse(r io.Reader) ([]stats.Sample, int, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // the title row has a different column count
	cr.LazyQuotes = true

	var header []string
	tsCol := -1
	var cols []glucoseCol // historic and scan columns, checked in order; a row uses whichever is non-empty
	samples := make([]stats.Sample, 0, 1024)
	skipped := 0

	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			skipped++
			continue
		}

		if header == nil {
			if col := findCol(row, "device timestamp"); col >= 0 {
				header, tsCol = row, col
				cols = findGlucoseCols(row)
			}
			continue // title row, or not the header row yet
		}
		if tsCol >= len(row) {
			skipped++
			continue
		}

		raw, mmol, found := "", false, false
		for _, c := range cols {
			if c.index >= len(row) {
				continue
			}
			if v := strings.TrimSpace(row[c.index]); v != "" {
				raw, mmol, found = v, c.mmol, true
				break
			}
		}
		if !found {
			continue // a non-glucose row (insulin, note, ...) sharing this column layout
		}
		val, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err != nil {
			skipped++
			continue
		}
		if mmol {
			val *= stats.MmolFactor
		}

		ts, ok := parseLibreTime(strings.TrimSpace(row[tsCol]))
		if !ok {
			skipped++
			continue
		}
		samples = append(samples, stats.Sample{Time: ts, Value: val})
	}

	if header == nil {
		return nil, 0, errNoHeader
	}
	return samples, skipped, nil
}

var errNoHeader = fmt.Errorf(`libre: no "Device Timestamp" column found; is this a LibreView glucose export?`)

func findCol(row []string, name string) int {
	for i, c := range row {
		if strings.EqualFold(strings.TrimSpace(c), name) {
			return i
		}
	}
	return -1
}

type glucoseCol struct {
	index int
	mmol  bool
}

// findGlucoseCols finds the historic (automatic) and scan (manual) glucose
// columns present in the header, historic first: a Libre CSV row has at
// most one of them non-empty (a reading is either an automatic sample or a
// manual scan, never both), so a row uses whichever one it finds filled in.
func findGlucoseCols(row []string) []glucoseCol {
	var cols []glucoseCol
	for _, want := range []struct {
		name string
		mmol bool
	}{
		{"historic glucose mg/dl", false},
		{"historic glucose mmol/l", true},
		{"scan glucose mg/dl", false},
		{"scan glucose mmol/l", true},
	} {
		if c := findCol(row, want.name); c >= 0 {
			cols = append(cols, glucoseCol{c, want.mmol})
		}
	}
	return cols
}

func parseLibreTime(s string) (time.Time, bool) {
	for _, layout := range libreTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
