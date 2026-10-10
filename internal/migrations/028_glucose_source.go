package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up028, nil, "028_glucose_source.go")
}

// up028 lets the live glucose source be chosen in Settings. glucose_source is
// the source id (empty means GLUCAVA_SOURCE, then Dexcom). source_config holds
// the non-secret settings of the sources as one JSON object, keyed "id.field";
// Dexcom keeps its own dexcom_region and dexcom_username columns.
func up028(app core.App) error {
	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	col.Fields.Add(
		&core.TextField{Name: "glucose_source", Max: 40},
		&core.TextField{Name: "source_config", Max: 8000},
	)
	return app.Save(col)
}
