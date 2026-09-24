package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up006, nil, "006_smtp_settings.go")
}

// up006 adds the SMTP settings for the email notify channel. The password is
// not here: it lives in the encrypted vault like the other secrets.
func up006(app core.App) error {
	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	col.Fields.Add(
		&core.TextField{Name: "smtp_host"},
		&core.NumberField{Name: "smtp_port", Min: ptr(0.0), OnlyInt: true},
		&core.TextField{Name: "smtp_username"},
		&core.BoolField{Name: "smtp_tls"},
		&core.TextField{Name: "smtp_sender_address"},
		&core.TextField{Name: "smtp_sender_name"},
	)
	return app.Save(col)
}
