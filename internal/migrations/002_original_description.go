package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up002, nil, "002_original_description.go")
}

// up002 lets an activity remember the description it had before Glucava first
// edited it, so the edit can be undone.
func up002(app core.App) error {
	col, err := app.FindCollectionByNameOrId("activities")
	if err != nil {
		return err
	}
	col.Fields.Add(
		&core.BoolField{Name: "has_original"},
		&core.TextField{Name: "original_description", Max: 1 << 16},
	)
	return app.Save(col)
}
