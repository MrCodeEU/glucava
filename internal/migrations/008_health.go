package migrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

func init() {
	core.AppMigrations.Register(up008, nil, "008_health.go")
}

// DefaultGapAlertHours is how long glucose readings may be missing before
// glucava raises an alert. 0 turns the alert off.
const DefaultGapAlertHours = 3

// up008 adds the glucose-gap alert threshold, the monthly health mail switch
// and its bookkeeping, and the glucose_gap event type.
func up008(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(
		&core.NumberField{Name: "gap_alert_hours", Min: ptr(0.0), OnlyInt: true},
		&core.BoolField{Name: "mail_health"},
		&core.TextField{Name: "mail_health_last"},
	)
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("gap_alert_hours", DefaultGapAlertHours)
		if err := app.Save(r); err != nil {
			return err
		}
	}

	events, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		return err
	}
	typ, ok := events.Fields.GetByName("type").(*core.SelectField)
	if !ok {
		return errors.New("migrations: events.type is not a select field")
	}
	typ.Values = append(typ.Values, "glucose_gap")
	return app.Save(events)
}
