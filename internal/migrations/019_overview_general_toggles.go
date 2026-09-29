package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up019, nil, "019_overview_general_toggles.go")
}

// up019 adds two more stats overview toggles: a whole-range glucose summary
// (not scoped to activities) and a per-source health list (last reading and
// count per source, so a live connection or an import's success is visible
// from the page, not just inferred from activities showing up). Same
// default-on reasoning as 018: new content, should show up rather than need
// finding.
func up019(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(
		&core.BoolField{Name: "overview_show_general"},
		&core.BoolField{Name: "overview_show_source_health"},
	)
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("overview_show_general", true)
		r.Set("overview_show_source_health", true)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
