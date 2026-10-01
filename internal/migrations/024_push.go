package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up024, nil, "024_push.go")
}

// up024 adds Web Push: the push_subscriptions collection (one row per
// subscribed browser or phone) and one switch per kind of push. As with
// email, failure alerts default on and summaries are opt-in; a switch only
// matters once a device is subscribed.
func up024(app core.App) error {
	subs := base("push_subscriptions",
		&core.TextField{Name: "endpoint", Required: true},
		&core.TextField{Name: "p256dh", Required: true},
		&core.TextField{Name: "auth", Required: true},
		&core.TextField{Name: "user_agent"},
		&core.DateField{Name: "last_ok"},
		&core.TextField{Name: "last_error"},
	)
	subs.AddIndex("idx_push_subscriptions_endpoint", true, "endpoint", "")
	if err := app.Save(subs); err != nil {
		return err
	}

	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(
		&core.BoolField{Name: "push_alerts"},
		&core.BoolField{Name: "push_summaries"},
	)
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("push_alerts", true)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
