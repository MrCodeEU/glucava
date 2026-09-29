package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up020, nil, "020_activity_distance_elevation.go")
}

// up020 adds the per-activity distance/elevation totals (from Strava's own
// listing, meters), the elevation profile kept for the chart (same shape as
// 011's heart_rate field), and the elevation overlay switch (on by default,
// same reasoning as chart_hr in 011: it only matters when the chart photo is
// on, and existing deployments should see it show up rather than need
// finding).
func up020(app core.App) error {
	acts, err := app.FindCollectionByNameOrId("activities")
	if err != nil {
		return err
	}
	acts.Fields.Add(
		&core.NumberField{Name: "distance_m"},
		&core.NumberField{Name: "elevation_gain_m"},
		&core.JSONField{Name: "elevation", MaxSize: 1 << 20},
	)
	if err := app.Save(acts); err != nil {
		return err
	}

	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(&core.BoolField{Name: "chart_elevation"})
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("chart_elevation", true)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
