package importers

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

func init() {
	Register("glooko", glookoImporter{})
}

// glookoImporter reads a Glooko CGM export (glookodata.com: Export data),
// either the zip download directly or cgm_data_*.csv from its extracted
// folder. Verified against a real export (German locale: "Zeitstempel",
// "CGM-Glukosewert (mg/dl)"), UTF-8 with a BOM. Other locales are expected
// to use the same layout with translated column names ("Timestamp", "CGM
// Glucose Value (mg/dl)"), so this looks up columns by keyword rather than a
// fixed name or position, the same defensive approach as libreImporter;
// report a mismatch with a redacted sample of your header row.
//
// Glooko's bg_data_*.csv (meter/fingerstick readings) is a different file
// and intentionally not read here: those are manual calibration checks, not
// continuous sensor data, the same distinction as Nightscout's mbg entries.
// A zip's other files (Manual data/, Insulin data/, ...) are ignored, not
// walked or extracted: see openZipMember for why that is safe by
// construction rather than by filtering.
type glookoImporter struct{}

// AcceptsZip marks glookoImporter as understanding a zip directly (see
// importers.ZipAware).
func (glookoImporter) AcceptsZip() bool { return true }

var glookoTimeLayouts = []string{
	"02.01.2006 15:04",
	"01/02/2006 15:04",
	"2006-01-02 15:04",
}

func (glookoImporter) Parse(r io.Reader) ([]stats.Sample, int, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxZipMember+1))
	if err != nil {
		return nil, 0, err
	}
	if len(data) > maxZipMember {
		return nil, 0, fmt.Errorf("glooko: file is larger than %d bytes", maxZipMember)
	}
	if looksLikeZip(data) {
		member, _, err := openZipMember(data, func(n string) bool {
			base := strings.ToLower(n)
			return strings.Contains(base, "cgm_data") && strings.HasSuffix(base, ".csv")
		})
		if err != nil {
			return nil, 0, fmt.Errorf("glooko: %w (looking for a cgm_data_*.csv entry)", err)
		}
		data = member
	}

	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1 // the title row has a different column count

	var header []string
	tsCol, valCol, mmol := -1, -1, false
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
			if ts := findColContains(row, "zeitstempel", "timestamp"); ts >= 0 {
				if v, isMmol := findGlucoseValueCol(row); v >= 0 {
					header, tsCol, valCol, mmol = row, ts, v, isMmol
				}
			}
			continue // title row, or not the header row yet
		}
		if tsCol >= len(row) || valCol >= len(row) {
			skipped++
			continue
		}

		raw := strings.TrimSpace(row[valCol])
		if raw == "" {
			skipped++
			continue
		}
		val, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err != nil {
			skipped++
			continue
		}
		if mmol {
			val *= stats.MmolFactor
		}

		ts, ok := parseGlookoTime(strings.TrimSpace(row[tsCol]))
		if !ok {
			skipped++
			continue
		}
		samples = append(samples, stats.Sample{Time: ts, Value: val})
	}

	if header == nil {
		return nil, 0, fmt.Errorf("glooko: no timestamp column found; is this a Glooko cgm_data export?")
	}
	return samples, skipped, nil
}

func findColContains(row []string, substrs ...string) int {
	for i, c := range row {
		low := strings.ToLower(strings.TrimSpace(c))
		for _, s := range substrs {
			if strings.Contains(low, s) {
				return i
			}
		}
	}
	return -1
}

// findGlucoseValueCol looks for the CGM value column specifically (not a
// meter/manual reading column, if the file ever had one), and reports
// whether it is mmol/L rather than mg/dL.
func findGlucoseValueCol(row []string) (col int, mmol bool) {
	for i, c := range row {
		low := strings.ToLower(strings.TrimSpace(c))
		if !strings.Contains(low, "glukosewert") && !strings.Contains(low, "glucose") {
			continue
		}
		switch {
		case strings.Contains(low, "mmol"):
			return i, true
		case strings.Contains(low, "mg/dl"):
			return i, false
		}
	}
	return -1, false
}

func parseGlookoTime(s string) (time.Time, bool) {
	for _, layout := range glookoTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
