package importers

import (
	"encoding/json"
	"io"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

func init() {
	Register("nightscout", nightscoutImporter{})
}

// nightscoutImporter reads a Nightscout entries.json export (the response of
// GET /api/v1/entries.json, or a file saved from it): a JSON array of entry
// objects. Nightscout's entries API is a long-stable, well-documented
// format: https://github.com/nightscout/cgm-remote-monitor/wiki/Latest-API.
// sgv entries are the CGM's own readings in mg/dL already; mbg (a manual
// fingerstick calibration) and cal (calibration coefficients) entries are
// intentionally skipped, not glucose readings from the sensor.
type nightscoutImporter struct{}

type nightscoutEntry struct {
	Type string  `json:"type"`
	SGV  float64 `json:"sgv"`
	Date float64 `json:"date"` // epoch milliseconds
}

func (nightscoutImporter) Parse(r io.Reader) ([]stats.Sample, int, error) {
	var entries []nightscoutEntry
	if err := json.NewDecoder(r).Decode(&entries); err != nil {
		return nil, 0, err
	}
	samples := make([]stats.Sample, 0, len(entries))
	skipped := 0
	for _, e := range entries {
		if e.Type != "sgv" {
			continue // not a sensor reading (mbg, cal, note, ...)
		}
		if e.Date <= 0 || e.SGV <= 0 {
			skipped++
			continue
		}
		samples = append(samples, stats.Sample{
			Time:  time.UnixMilli(int64(e.Date)),
			Value: e.SGV,
		})
	}
	return samples, skipped, nil
}
