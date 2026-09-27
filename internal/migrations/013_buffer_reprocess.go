package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up013, nil, "013_buffer_reprocess.go")
}

// DefaultPostBufferMinutes is how long after the glucose window closes an
// activity is automatically reprocessed once, to pick up Dexcom readings
// that had not arrived yet when it was first processed.
const DefaultPostBufferMinutes = 5

// up013 adds the delayed-reprocess buffer setting and the per-activity flag
// that makes it run at most once.
func up013(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(&core.NumberField{Name: "post_buffer_minutes", Min: ptr(0.0), Max: ptr(180.0), OnlyInt: true})
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("post_buffer_minutes", DefaultPostBufferMinutes)
		if err := app.Save(r); err != nil {
			return err
		}
	}

	activities, err := app.FindCollectionByNameOrId("activities")
	if err != nil {
		return err
	}
	activities.Fields.Add(&core.BoolField{Name: "buffer_done"})
	return app.Save(activities)
}
