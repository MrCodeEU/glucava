package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up018, nil, "018_overview_toggles.go")
}

// up018 adds the three show/hide toggles on the new stats overview page
// (trend chart, by-activity-type breakdown, raw activity table). All three
// default to on, unlike the chart overlay toggles in 017: this is new
// content nobody has seen before, so an existing deployment should see it
// show up rather than have to go find the switch.
func up018(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(
		&core.BoolField{Name: "overview_show_trend"},
		&core.BoolField{Name: "overview_show_by_sport"},
		&core.BoolField{Name: "overview_show_table"},
	)
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("overview_show_trend", true)
		r.Set("overview_show_by_sport", true)
		r.Set("overview_show_table", true)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
