package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up007, nil, "007_mail_kinds.go")
}

// up007 adds the public URL (for links in notifications) and one switch per
// kind of email. Failure alerts stay on, as they were before; the summaries
// are opt-in. mail_weekly_last is internal bookkeeping for the weekly digest.
func up007(app core.App) error {
	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	col.Fields.Add(
		&core.TextField{Name: "public_url"},
		&core.BoolField{Name: "mail_alerts"},
		&core.BoolField{Name: "mail_activity"},
		&core.BoolField{Name: "mail_weekly"},
		&core.TextField{Name: "mail_weekly_last"},
	)
	if err := app.Save(col); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("mail_alerts", true)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
