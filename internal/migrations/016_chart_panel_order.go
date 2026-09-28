package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up016, nil, "016_chart_panel_order.go")
}

// up016 adds the chart panel draw order: a comma-separated list of "activity",
// "band", "dots", "hr". Empty means the default order (unchanged from
// before this existed), so every existing deployment's chart looks exactly
// the same until someone reorders it.
func up016(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(&core.TextField{Name: "chart_panel_order"})
	return app.Save(settings)
}
