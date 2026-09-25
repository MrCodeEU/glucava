package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up011, nil, "011_chart_hr.go")
}

// up011 adds the heart rate overlay switch (on by default, it only matters
// when the chart photo is on) and the per-activity heart rate kept for charts.
func up011(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(&core.BoolField{Name: "chart_hr"})
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("chart_hr", true)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	acts, err := app.FindCollectionByNameOrId("activities")
	if err != nil {
		return err
	}
	acts.Fields.Add(&core.JSONField{Name: "heart_rate", MaxSize: 1 << 20})
	return app.Save(acts)
}
