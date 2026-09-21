// Package bootstrap holds first-run setup.
package bootstrap

import (
	"errors"
	"log"
	"os"

	"github.com/pocketbase/pocketbase/core"
)

// EnsureAdminUser creates the first web UI user from GLUCAVA_ADMIN_EMAIL and
// GLUCAVA_ADMIN_PASSWORD when no user exists yet. It does nothing once a
// user exists or when the variables are unset.
func EnsureAdminUser(app core.App) error {
	email := os.Getenv("GLUCAVA_ADMIN_EMAIL")
	password := os.Getenv("GLUCAVA_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return nil
	}
	n, err := app.CountRecords("users")
	if err != nil || n > 0 {
		return err
	}
	if len(password) < 12 {
		return errors.New("bootstrap: GLUCAVA_ADMIN_PASSWORD must be at least 12 characters")
	}
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	rec := core.NewRecord(users)
	rec.SetEmail(email)
	rec.SetPassword(password)
	rec.SetVerified(true)
	if err := app.Save(rec); err != nil {
		return err
	}
	log.Printf("bootstrap: created first user %s", email)
	return nil
}

// EnforceSingleUser rejects creating a second user. Glucava keeps one person's
// medical data, and every account sees all of it.
func EnforceSingleUser(app core.App) {
	app.OnRecordCreate("users").BindFunc(func(e *core.RecordEvent) error {
		n, err := e.App.CountRecords("users")
		if err != nil {
			return err
		}
		if n > 0 {
			return errors.New("glucava supports a single user account")
		}
		return e.Next()
	})
}
