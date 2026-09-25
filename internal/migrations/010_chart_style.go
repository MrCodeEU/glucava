package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up010, nil, "010_chart_style.go")
}

// up010 adds the look of the chart photo. Defaults match the chart glucava
// already draws in emails.
func up010(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(
		&core.TextField{Name: "chart_theme"},
		&core.TextField{Name: "chart_size"},
		&core.BoolField{Name: "chart_band"},
		&core.BoolField{Name: "chart_activity"},
		&core.BoolField{Name: "chart_dots"},
		&core.NumberField{Name: "chart_line", Min: ptr(1.0), Max: ptr(4.0), OnlyInt: true},
	)
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("chart_theme", "light")
		r.Set("chart_size", "standard")
		r.Set("chart_band", true)
		r.Set("chart_activity", true)
		r.Set("chart_dots", true)
		r.Set("chart_line", 2)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
