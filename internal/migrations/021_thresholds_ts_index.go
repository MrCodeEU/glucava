package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up021, nil, "021_thresholds_ts_index.go")
}

// up021 adds an index on glucose_samples(ts) (the only index was
// (source, ts), which cannot serve a range scan across sources) and the
// configurable very-low/very-high thresholds, defaulting to the 54/250 mg/dL
// consensus values that were hard-coded before.
func up021(app core.App) error {
	samples, err := app.FindCollectionByNameOrId("glucose_samples")
	if err != nil {
		return err
	}
	samples.AddIndex("idx_glucose_samples_ts", false, "ts", "")
	if err := app.Save(samples); err != nil {
		return err
	}

	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(
		&core.NumberField{Name: "very_low"},
		&core.NumberField{Name: "very_high"},
	)
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("very_low", 54)
		r.Set("very_high", 250)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
