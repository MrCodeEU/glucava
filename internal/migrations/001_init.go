// Package migrations defines the PocketBase collections. Importing it for side
// effects registers the migrations, which PocketBase runs at startup.
package migrations

import (
	"github.com/pocketbase/pocketbase/core"
)

func init() {
	core.AppMigrations.Register(up001, nil, "001_init.go")
}

// All collections use nil API rules, so only superusers and server code can
// reach them through the PocketBase API. The web UI reads them server-side.
func up001(app core.App) error {
	if err := lockDownUsers(app); err != nil {
		return err
	}

	activities := base("activities",
		&core.TextField{Name: "strava_id", Required: true},
		&core.TextField{Name: "name"},
		&core.TextField{Name: "sport_type"},
		&core.DateField{Name: "start_time"},
		&core.NumberField{Name: "duration_sec"},
		&core.SelectField{Name: "status", Required: true, MaxSelect: 1,
			Values: []string{"pending", "processing", "done", "failed", "skipped"}},
		&core.TextField{Name: "error"},
		&core.NumberField{Name: "attempts"},
		&core.JSONField{Name: "summary", MaxSize: 1 << 16},
		&core.DateField{Name: "processed_at"},
	)
	activities.AddIndex("idx_activities_strava_id", true, "strava_id", "")
	activities.AddIndex("idx_activities_status", false, "status", "")

	samples := base("glucose_samples",
		&core.DateField{Name: "ts", Required: true},
		&core.NumberField{Name: "value", Required: true}, // mg/dL
		&core.TextField{Name: "source", Required: true},
	)
	samples.AddIndex("idx_glucose_samples_source_ts", true, "source, ts", "")

	events := base("events",
		&core.SelectField{Name: "type", Required: true, MaxSelect: 1, Values: []string{
			"strava_failed", "selector_repaired", "session_expired",
			"glucose_unavailable", "canary_failed", "trigger_rejected",
		}},
		&core.SelectField{Name: "severity", Required: true, MaxSelect: 1,
			Values: []string{"info", "warning", "error"}},
		&core.TextField{Name: "message"},
		&core.BoolField{Name: "repaired"},
		&core.TextField{Name: "strava_id"},
		&core.JSONField{Name: "data", MaxSize: 1 << 16},
		&core.BoolField{Name: "notified"},
	)
	events.AddIndex("idx_events_type", false, "type", "")

	tokens := base("api_tokens",
		&core.TextField{Name: "name", Required: true},
		&core.TextField{Name: "token_hash", Required: true}, // sha256 hex, never the token
		&core.DateField{Name: "last_used"},
		&core.BoolField{Name: "revoked"},
	)
	tokens.AddIndex("idx_api_tokens_hash", true, "token_hash", "")

	secrets := base("secrets",
		&core.TextField{Name: "name", Required: true},
		&core.TextField{Name: "ciphertext", Required: true}, // encrypted by internal code
	)
	secrets.AddIndex("idx_secrets_name", true, "name", "")

	selectors := base("selector_cache",
		&core.TextField{Name: "key", Required: true},
		&core.TextField{Name: "selector", Required: true},
		&core.SelectField{Name: "source", Required: true, MaxSelect: 1, Values: []string{"builtin", "llm"}},
		&core.DateField{Name: "verified_at"},
	)
	selectors.AddIndex("idx_selector_cache_key", true, "key", "")

	settings := base("settings",
		&core.SelectField{Name: "unit", MaxSelect: 1, Values: []string{"mg/dL", "mmol/L"}},
		&core.NumberField{Name: "range_low"},
		&core.NumberField{Name: "range_high"},
		&core.NumberField{Name: "pre_minutes"},
		&core.NumberField{Name: "post_minutes"},
		&core.NumberField{Name: "poll_interval_minutes"},
		&core.BoolField{Name: "image_enabled"},
		&core.SelectField{Name: "dexcom_region", MaxSelect: 1, Values: []string{"us", "ous", "jp"}},
		&core.TextField{Name: "dexcom_username"},
		&core.TextField{Name: "ntfy_url"},
		&core.TextField{Name: "webhook_url"},
		&core.BoolField{Name: "llm_enabled"},
		&core.TextField{Name: "llm_url"},
		&core.TextField{Name: "llm_model"},
		&core.SelectField{Name: "lang", MaxSelect: 1, Values: []string{"en", "de"}},
	)

	for _, c := range []*core.Collection{activities, samples, events, tokens, secrets, selectors, settings} {
		if err := app.Save(c); err != nil {
			return err
		}
	}

	// Singleton settings row with defaults.
	rec := core.NewRecord(settings)
	rec.Set("unit", "mg/dL")
	rec.Set("range_low", 70)
	rec.Set("range_high", 180)
	rec.Set("pre_minutes", 0)
	rec.Set("post_minutes", 30)
	rec.Set("poll_interval_minutes", 10)
	rec.Set("dexcom_region", "ous")
	rec.Set("lang", "en")
	return app.Save(rec)
}

func base(name string, fields ...core.Field) *core.Collection {
	c := core.NewBaseCollection(name)
	c.Fields.Add(fields...)
	c.Fields.Add(
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	return c
}

// lockDownUsers turns off public sign-up on the built-in users collection.
// Accounts are created by the bootstrap step or by a superuser.
func lockDownUsers(app core.App) error {
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	users.CreateRule = nil
	users.ListRule = nil
	users.ViewRule = nil
	users.UpdateRule = nil
	users.DeleteRule = nil
	return app.Save(users)
}
