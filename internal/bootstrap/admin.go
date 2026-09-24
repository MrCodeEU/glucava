// Package bootstrap holds first-run setup.
package bootstrap

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

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

// ApplyRetentionOverride sets retention_days from GLUCAVA_RETENTION_DAYS
// when it is set, every start. Unlike EnsureDexcomCredential this is not a
// one-time seed: retention has no "unconfigured" value to detect (0 and any
// positive number of days are both meaningful settings a person may have
// chosen in the UI), so there is no way to tell "still the default" apart
// from "deliberately set back to it". An env var that always wins is the
// honest behaviour; remove it from .env to let the UI value stick again.
func ApplyRetentionOverride(app core.App) error {
	raw := os.Getenv("GLUCAVA_RETENTION_DAYS")
	if raw == "" {
		return nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 0 {
		return fmt.Errorf("bootstrap: GLUCAVA_RETENTION_DAYS must be a non-negative integer, got %q", raw)
	}
	recs, err := app.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil || len(recs) == 0 {
		return fmt.Errorf("bootstrap: settings row not found: %w", err)
	}
	if recs[0].GetInt("retention_days") == days {
		return nil
	}
	recs[0].Set("retention_days", days)
	if err := app.Save(recs[0]); err != nil {
		return err
	}
	log.Printf("bootstrap: retention set to %d days from the environment", days)
	return nil
}

// EnsureSMTP seeds the email notify SMTP settings from GLUCAVA_SMTP_* when
// none are stored yet. Like EnsureDexcomCredential it only ever seeds a
// first value: once smtp_host is set (by env or the web UI) the variables
// are ignored on later starts, so UI edits are not overwritten and the
// password can be removed from .env again. GLUCAVA_SMTP_HOST and
// GLUCAVA_SMTP_SENDER_ADDRESS are required to seed.
func EnsureSMTP(app core.App, vault Vault, passwordName string) error {
	host := os.Getenv("GLUCAVA_SMTP_HOST")
	if host == "" {
		return nil
	}
	senderAddr := os.Getenv("GLUCAVA_SMTP_SENDER_ADDRESS")
	if senderAddr == "" {
		return fmt.Errorf("bootstrap: GLUCAVA_SMTP_SENDER_ADDRESS is required when GLUCAVA_SMTP_HOST is set")
	}
	port := 587
	if raw := os.Getenv("GLUCAVA_SMTP_PORT"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil || p <= 0 || p > 65535 {
			return fmt.Errorf("bootstrap: GLUCAVA_SMTP_PORT must be a port number, got %q", raw)
		}
		port = p
	}
	senderName := os.Getenv("GLUCAVA_SMTP_SENDER_NAME")
	if senderName == "" {
		senderName = "glucava"
	}
	recs, err := app.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil || len(recs) == 0 {
		return fmt.Errorf("bootstrap: settings row not found: %w", err)
	}
	if recs[0].GetString("smtp_host") != "" {
		return nil // already configured (env earlier, or the web UI)
	}
	recs[0].Set("smtp_host", host)
	recs[0].Set("smtp_port", port)
	recs[0].Set("smtp_username", os.Getenv("GLUCAVA_SMTP_USERNAME"))
	recs[0].Set("smtp_tls", os.Getenv("GLUCAVA_SMTP_TLS") == "1")
	recs[0].Set("smtp_sender_address", senderAddr)
	recs[0].Set("smtp_sender_name", senderName)
	if err := app.Save(recs[0]); err != nil {
		return err
	}
	if pw := os.Getenv("GLUCAVA_SMTP_PASSWORD"); pw != "" {
		if err := vault.Set(passwordName, pw); err != nil {
			return err
		}
	}
	log.Printf("bootstrap: SMTP configured from the environment (%s:%d)", host, port)
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
