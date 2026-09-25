package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up009, nil, "009_chart_image.go")
}

// up009 adds the opt-in switch for attaching a glucose chart to the Strava
// activity, and a per-activity flag so a reprocess never attaches it twice
// (glucava cannot remove a photo again).
func up009(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(&core.BoolField{Name: "chart_image"})
	if err := app.Save(settings); err != nil {
		return err
	}
	acts, err := app.FindCollectionByNameOrId("activities")
	if err != nil {
		return err
	}
	acts.Fields.Add(&core.BoolField{Name: "chart_uploaded"})
	return app.Save(acts)
}
