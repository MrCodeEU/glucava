package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up012, nil, "012_hr_and_chart_pre.go")
}

// DefaultChartPreMinutes is how much glucose before the activity the chart
// photo shows, independent of the window used for the statistics.
const DefaultChartPreMinutes = 30

// up012 adds the switch for reading heart rate from Strava (on by default) and
// the chart's own lead-in.
func up012(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(
		&core.BoolField{Name: "hr_read"},
		&core.NumberField{Name: "chart_pre_minutes", Min: ptr(0.0), Max: ptr(240.0), OnlyInt: true},
	)
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("hr_read", true)
		r.Set("chart_pre_minutes", DefaultChartPreMinutes)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
