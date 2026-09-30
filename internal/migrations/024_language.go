package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up024, nil, "024_language.go")
}

// up024 adds the interface language setting. "auto" (the default for every
// existing installation) follows the browser's Accept-Language header; any
// other value is a locale tag such as "de", validated against the embedded
// locale files by store.Config.Validate, not here, so adding a language never
// needs a migration.
func up024(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(&core.TextField{Name: "language", Max: 35})
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("language", "auto")
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
