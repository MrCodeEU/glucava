package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up026, nil, "026_event_message_key.go")
}

// up026 lets a stored event carry a translation key and its arguments next to
// the English message. The Notifications page and the notification texts render
// the key in the reader's language; rows written before this migration have no
// key and keep showing the English message.
//
// msg_args is a JSON object in a text field (string, number and bool values
// only). Durations are whole minutes and times are RFC 3339 so the stored
// arguments do not depend on a locale; the text is formatted when it is shown.
func up026(app core.App) error {
	events, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		return err
	}
	events.Fields.Add(
		&core.TextField{Name: "msg_key", Max: 120},
		&core.TextField{Name: "msg_args", Max: 4000},
	)
	return app.Save(events)
}
