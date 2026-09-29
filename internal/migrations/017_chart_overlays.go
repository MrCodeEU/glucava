package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up017, nil, "017_chart_overlays.go")
}

// up017 adds the schema fields for the four chart overlay toggles (average
// line, target range low/high lines, min/max markers, hide stats) the
// Settings page has shown since 0.3.0. Without these fields the settings
// record silently dropped every write to them: LoadConfig/SaveConfig and
// Settings() already read and wrote these column names, but the columns
// themselves never existed, so the toggles never actually persisted or
// reached the chart photo attached to Strava. All default off, matching the
// off-by-default behaviour every deployment already had.
func up017(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(
		&core.BoolField{Name: "chart_avg_line"},
		&core.BoolField{Name: "chart_range_lines"},
		&core.BoolField{Name: "chart_min_max"},
		&core.BoolField{Name: "chart_hide_stats"},
	)
	return app.Save(settings)
}
