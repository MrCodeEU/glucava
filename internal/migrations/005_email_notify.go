package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up005, nil, "005_email_notify.go")
}

// up005 adds the email notify recipient. Email is sent through PocketBase's
// own mailer (SMTP configured via GLUCAVA_SMTP_* env vars, see
// bootstrap.ApplySMTPOverride); glucava only stores who it goes to.
func up005(app core.App) error {
	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	col.Fields.Add(&core.TextField{Name: "email_to"})
	return app.Save(col)
}
