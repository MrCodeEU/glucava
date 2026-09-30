package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up023, nil, "023_artifact_marks.go")
}

// up023 adds manual verdicts on suspected sensor artifacts (artifact_marks:
// a time span marked "artifact" = not real, or "real") and the setting that
// chooses whether suspected artifacts are only flagged or also left out of
// the statistics. The default keeps every number as it was.
func up023(app core.App) error {
	marks := base("artifact_marks",
		&core.DateField{Name: "start", Required: true},
		&core.DateField{Name: "end", Required: true},
		&core.SelectField{Name: "kind", Required: true, MaxSelect: 1, Values: []string{"artifact", "real"}},
	)
	marks.AddIndex("idx_artifact_marks_start", false, "start", "")
	if err := app.Save(marks); err != nil {
		return err
	}

	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(&core.SelectField{Name: "artifact_mode", MaxSelect: 1, Values: []string{"flagged", "exclude"}})
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("artifact_mode", "flagged")
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
