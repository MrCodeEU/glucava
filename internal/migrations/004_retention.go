package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up004, nil, "004_retention.go")
}

// DefaultRetentionDays is how long glucose samples and events are kept. 0 keeps them forever.
const DefaultRetentionDays = 365

// up004 adds the retention setting.
func up004(app core.App) error {
	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	col.Fields.Add(&core.NumberField{Name: "retention_days", Min: ptr(0.0), OnlyInt: true})
	if err := app.Save(col); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("retention_days", DefaultRetentionDays)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
