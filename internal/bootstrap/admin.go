// Package bootstrap holds first-run setup.
package bootstrap

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/pocketbase/dbx"
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

// Vault is the subset of *secrets.Vault that EnsureDexcomCredential needs.
type Vault interface {
	Get(name string) (string, bool, error)
	Set(name, value string) error
}

// EnsureDexcomCredential stores Dexcom Share credentials from
// GLUCAVA_DEXCOM_USERNAME, GLUCAVA_DEXCOM_PASSWORD and GLUCAVA_DEXCOM_REGION
// when none are stored yet. Like EnsureAdminUser, this only ever seeds a
// first value; once a credential exists (by any means: env, CLI, or the web
// UI) the env vars are ignored on every later start, so the password can be
// removed from .env again after the first run.
func EnsureDexcomCredential(app core.App, vault Vault, dexcomPasswordName string) error {
	user := os.Getenv("GLUCAVA_DEXCOM_USERNAME")
	password := os.Getenv("GLUCAVA_DEXCOM_PASSWORD")
	region := os.Getenv("GLUCAVA_DEXCOM_REGION")
	if user == "" || password == "" {
		return nil
	}
	if region != "us" && region != "ous" && region != "jp" {
		return fmt.Errorf(`bootstrap: GLUCAVA_DEXCOM_REGION must be "us", "ous" or "jp", got %q`, region)
	}
	if _, ok, err := vault.Get(dexcomPasswordName); err != nil || ok {
		return err
	}
	recs, err := app.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil || len(recs) == 0 {
		return fmt.Errorf("bootstrap: settings row not found: %w", err)
	}
	if recs[0].GetString("dexcom_username") != "" {
		return nil // already configured through the UI/CLI, just not the vault we checked
	}
	recs[0].Set("dexcom_username", user)
	recs[0].Set("dexcom_region", region)
	if err := app.Save(recs[0]); err != nil {
		return err
	}
	if err := vault.Set(dexcomPasswordName, password); err != nil {
		return err
	}
	log.Printf("bootstrap: stored Dexcom credentials for %s (region %s) from the environment", user, region)
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

// SilenceSuperuserPrompt creates a superuser with a random, discarded password
// if none exists. Glucava's own account is the "users" collection, not
// PocketBase superusers, and the admin UI/API are blocked (see
// cmd/glucava blockPocketBase), so this account is unreachable. It exists
// only to stop PocketBase's installer banner, which otherwise prints a
// working one-time setup link to the log on every start.
func SilenceSuperuserPrompt(app core.App) error {
	// Match PocketBase's own installer check (apis.needInstallerSuperuser):
	// it excludes core.DefaultInstallerEmail, the record it creates for
	// itself to mint the one-time setup link. Counting all superusers here
	// would see that record and skip creating a real one, and the banner
	// would keep coming back forever.
	n, err := app.CountRecords(core.CollectionNameSuperusers, dbx.Not(dbx.HashExp{"email": core.DefaultInstallerEmail}))
	if err != nil || n > 0 {
		return err
	}
	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	pw, err := randomPassword()
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.SetEmail(fmt.Sprintf("glucava-internal-%d@localhost.invalid", os.Getpid()))
	rec.SetPassword(pw)
	return app.Save(rec)
}

func randomPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
