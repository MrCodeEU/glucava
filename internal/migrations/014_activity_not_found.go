package migrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

func init() {
	core.AppMigrations.Register(up014, nil, "014_activity_not_found.go")
}

// up014 adds the activity_not_found event type: a Writer's edit page
// redirected away from the activity instead of showing it, distinct from a
// selector failure (Strava's markup changed) or an expired session.
func up014(app core.App) error {
	events, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		return err
	}
	typ, ok := events.Fields.GetByName("type").(*core.SelectField)
	if !ok {
		return errors.New("migrations: events.type is not a select field")
	}
	typ.Values = append(typ.Values, "activity_not_found")
	return app.Save(events)
}
