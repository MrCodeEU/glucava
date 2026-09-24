package bootstrap

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"

	_ "github.com/MrCodeEU/glucava/internal/migrations"
	_ "github.com/pocketbase/pocketbase/migrations"
)

func newApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	return app
}

func newUser(app core.App, email string) error {
	users, _ := app.FindCollectionByNameOrId("users")
	r := core.NewRecord(users)
	r.SetEmail(email)
	r.SetPassword("a-long-enough-password")
	return app.Save(r)
}

func TestEnforceSingleUser(t *testing.T) {
	app := newApp(t)
	EnforceSingleUser(app)
	if err := newUser(app, "a@example.test"); err != nil {
		t.Fatalf("first user: %v", err)
	}
	if err := newUser(app, "b@example.test"); err == nil {
		t.Fatal("second user was created")
	}
}

func TestEnsureAdminUserRejectsShortPassword(t *testing.T) {
	app := newApp(t)
	t.Setenv("GLUCAVA_ADMIN_EMAIL", "a@example.test")
	t.Setenv("GLUCAVA_ADMIN_PASSWORD", "short")
	if err := EnsureAdminUser(app); err == nil {
		t.Error("short password accepted")
	}
	t.Setenv("GLUCAVA_ADMIN_PASSWORD", "long-enough-password")
	if err := EnsureAdminUser(app); err != nil {
		t.Fatal(err)
	}
	if n, _ := app.CountRecords("users"); n != 1 {
		t.Errorf("users = %d", n)
	}
}

func TestSilenceSuperuserPromptIsIdempotentAndOnlyOnce(t *testing.T) {
	app := newApp(t)
	if err := SilenceSuperuserPrompt(app); err != nil {
		t.Fatal(err)
	}
	n, err := app.CountRecords(core.CollectionNameSuperusers)
	if err != nil || n != 1 {
		t.Fatalf("superusers = %d, %v", n, err)
	}
	if err := SilenceSuperuserPrompt(app); err != nil {
		t.Fatal(err)
	}
	if n, _ := app.CountRecords(core.CollectionNameSuperusers); n != 1 {
		t.Errorf("superusers after second call = %d", n)
	}
}

// fakeVault is an in-memory stand-in for *secrets.Vault, just enough to
// exercise EnsureDexcomCredential without a real encryption key.
type fakeVault map[string]string

func (v fakeVault) Get(name string) (string, bool, error) {
	val, ok := v[name]
	return val, ok, nil
}

func (v fakeVault) Set(name, value string) error {
	v[name] = value
	return nil
}

func TestEnsureDexcomCredentialSeedsFromEnvOnce(t *testing.T) {
	app := newApp(t)
	v := fakeVault{}
	t.Setenv("GLUCAVA_DEXCOM_USERNAME", "me@example.test")
	t.Setenv("GLUCAVA_DEXCOM_PASSWORD", "s3cret-dexcom")
	t.Setenv("GLUCAVA_DEXCOM_REGION", "ous")

	if err := EnsureDexcomCredential(app, v, "dexcom_password"); err != nil {
		t.Fatal(err)
	}
	if v["dexcom_password"] != "s3cret-dexcom" {
		t.Fatalf("vault password = %q", v["dexcom_password"])
	}
	recs, err := app.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil || len(recs) == 0 {
		t.Fatalf("settings row: %v", err)
	}
	if recs[0].GetString("dexcom_username") != "me@example.test" || recs[0].GetString("dexcom_region") != "ous" {
		t.Errorf("settings not updated: %v", recs[0].PublicExport())
	}

	// A second start, even with different env values, must not overwrite
	// an already-stored credential.
	t.Setenv("GLUCAVA_DEXCOM_PASSWORD", "different-password")
	if err := EnsureDexcomCredential(app, v, "dexcom_password"); err != nil {
		t.Fatal(err)
	}
	if v["dexcom_password"] != "s3cret-dexcom" {
		t.Errorf("password was overwritten: %q", v["dexcom_password"])
	}
}

func TestEnsureDexcomCredentialRejectsBadRegion(t *testing.T) {
	app := newApp(t)
	v := fakeVault{}
	t.Setenv("GLUCAVA_DEXCOM_USERNAME", "me@example.test")
	t.Setenv("GLUCAVA_DEXCOM_PASSWORD", "s3cret-dexcom")
	t.Setenv("GLUCAVA_DEXCOM_REGION", "atlantis")

	if err := EnsureDexcomCredential(app, v, "dexcom_password"); err == nil {
		t.Error("bad region accepted")
	}
}

func TestApplyRetentionOverrideSetsFromEnv(t *testing.T) {
	app := newApp(t)
	t.Setenv("GLUCAVA_RETENTION_DAYS", "30")
	if err := ApplyRetentionOverride(app); err != nil {
		t.Fatal(err)
	}
	recs, err := app.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil || len(recs) == 0 || recs[0].GetInt("retention_days") != 30 {
		t.Fatalf("retention_days: %v, %v", recs, err)
	}

	// It applies every start, including changing an already-set value, unlike
	// EnsureDexcomCredential's seed-once behaviour.
	t.Setenv("GLUCAVA_RETENTION_DAYS", "0")
	if err := ApplyRetentionOverride(app); err != nil {
		t.Fatal(err)
	}
	recs, err = app.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil || len(recs) == 0 || recs[0].GetInt("retention_days") != 0 {
		t.Fatalf("retention_days after second call: %v, %v", recs, err)
	}
}

func TestApplyRetentionOverrideUnsetDoesNothing(t *testing.T) {
	app := newApp(t)
	if err := ApplyRetentionOverride(app); err != nil {
		t.Fatal(err)
	}
	recs, err := app.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil || len(recs) == 0 || recs[0].GetInt("retention_days") == 0 {
		t.Fatalf("retention_days changed with no env set: %v, %v", recs, err)
	}
}

func TestApplyRetentionOverrideRejectsBadValue(t *testing.T) {
	app := newApp(t)
	t.Setenv("GLUCAVA_RETENTION_DAYS", "-5")
	if err := ApplyRetentionOverride(app); err == nil {
		t.Error("negative value accepted")
	}
	t.Setenv("GLUCAVA_RETENTION_DAYS", "not-a-number")
	if err := ApplyRetentionOverride(app); err == nil {
		t.Error("non-numeric value accepted")
	}
}

func smtpRec(t *testing.T, app core.App) *core.Record {
	t.Helper()
	recs, err := app.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil || len(recs) == 0 {
		t.Fatalf("settings: %v", err)
	}
	return recs[0]
}

func TestEnsureSMTPUnsetDoesNothing(t *testing.T) {
	app := newApp(t)
	if err := EnsureSMTP(app, fakeVault{}, "smtp_password"); err != nil {
		t.Fatal(err)
	}
	if smtpRec(t, app).GetString("smtp_host") != "" {
		t.Error("host set with no env")
	}
}

func TestEnsureSMTPRequiresSenderAddress(t *testing.T) {
	app := newApp(t)
	t.Setenv("GLUCAVA_SMTP_HOST", "smtp.example.com")
	if err := EnsureSMTP(app, fakeVault{}, "smtp_password"); err == nil {
		t.Error("expected an error with no sender address")
	}
}

func TestEnsureSMTPSeedsOnceFromEnv(t *testing.T) {
	app := newApp(t)
	v := fakeVault{}
	t.Setenv("GLUCAVA_SMTP_HOST", "smtp.example.com")
	t.Setenv("GLUCAVA_SMTP_PORT", "2525")
	t.Setenv("GLUCAVA_SMTP_USERNAME", "user")
	t.Setenv("GLUCAVA_SMTP_PASSWORD", "pass")
	t.Setenv("GLUCAVA_SMTP_TLS", "1")
	t.Setenv("GLUCAVA_SMTP_SENDER_ADDRESS", "glucava@example.com")
	t.Setenv("GLUCAVA_SMTP_SENDER_NAME", "Glucava")
	t.Setenv("GLUCAVA_SMTP_TO", "me@example.com")
	if err := EnsureSMTP(app, v, "smtp_password"); err != nil {
		t.Fatal(err)
	}
	r := smtpRec(t, app)
	if r.GetString("smtp_host") != "smtp.example.com" || r.GetInt("smtp_port") != 2525 || r.GetString("smtp_username") != "user" ||
		!r.GetBool("smtp_tls") || r.GetString("email_to") != "me@example.com" || r.GetString("smtp_sender_address") != "glucava@example.com" || r.GetString("smtp_sender_name") != "Glucava" {
		t.Errorf("record = %+v", r.FieldsData())
	}
	if pw, ok, _ := v.Get("smtp_password"); !ok || pw != "pass" {
		t.Errorf("password = %q, %v", pw, ok)
	}

	// A later start must not overwrite what the UI has since changed.
	r.Set("smtp_host", "smtp.changed.example")
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSMTP(app, v, "smtp_password"); err != nil {
		t.Fatal(err)
	}
	if smtpRec(t, app).GetString("smtp_host") != "smtp.changed.example" {
		t.Error("env overwrote a configured host")
	}
}

func TestEnsureSMTPDefaultsPortAndSenderName(t *testing.T) {
	app := newApp(t)
	t.Setenv("GLUCAVA_SMTP_HOST", "smtp.example.com")
	t.Setenv("GLUCAVA_SMTP_SENDER_ADDRESS", "glucava@example.com")
	if err := EnsureSMTP(app, fakeVault{}, "smtp_password"); err != nil {
		t.Fatal(err)
	}
	r := smtpRec(t, app)
	if r.GetInt("smtp_port") != 587 || r.GetString("smtp_sender_name") != "glucava" {
		t.Errorf("port %d name %q", r.GetInt("smtp_port"), r.GetString("smtp_sender_name"))
	}
}

func TestEnsureSMTPRejectsBadPort(t *testing.T) {
	app := newApp(t)
	t.Setenv("GLUCAVA_SMTP_HOST", "smtp.example.com")
	t.Setenv("GLUCAVA_SMTP_SENDER_ADDRESS", "glucava@example.com")
	t.Setenv("GLUCAVA_SMTP_PORT", "not-a-number")
	if err := EnsureSMTP(app, fakeVault{}, "smtp_password"); err == nil {
		t.Error("non-numeric port accepted")
	}
}

func TestEnsureSMTPLateEnvPasswordIsStored(t *testing.T) {
	app := newApp(t)
	v := fakeVault{}
	t.Setenv("GLUCAVA_SMTP_HOST", "smtp.example.com")
	t.Setenv("GLUCAVA_SMTP_SENDER_ADDRESS", "glucava@example.com")
	if err := EnsureSMTP(app, v, "smtp_password"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := v.Get("smtp_password"); ok {
		t.Fatal("password stored without env")
	}
	// Host is saved now; a password added to .env later must still land.
	t.Setenv("GLUCAVA_SMTP_PASSWORD", "app-pass")
	if err := EnsureSMTP(app, v, "smtp_password"); err != nil {
		t.Fatal(err)
	}
	if pw, _, _ := v.Get("smtp_password"); pw != "app-pass" {
		t.Errorf("password = %q", pw)
	}
	// And it does not overwrite one that is already stored.
	t.Setenv("GLUCAVA_SMTP_PASSWORD", "other")
	_ = EnsureSMTP(app, v, "smtp_password")
	if pw, _, _ := v.Get("smtp_password"); pw != "app-pass" {
		t.Errorf("password overwritten: %q", pw)
	}
}

func TestEnsurePublicURL(t *testing.T) {
	app := newApp(t)
	if err := EnsurePublicURL(app); err != nil || smtpRec(t, app).GetString("public_url") != "" {
		t.Fatalf("unset env should do nothing: %v", err)
	}
	t.Setenv("GLUCAVA_PUBLIC_URL", "ftp://nope")
	if err := EnsurePublicURL(app); err == nil {
		t.Error("non-http URL accepted")
	}
	t.Setenv("GLUCAVA_PUBLIC_URL", "https://glucava.example.com")
	if err := EnsurePublicURL(app); err != nil {
		t.Fatal(err)
	}
	if got := smtpRec(t, app).GetString("public_url"); got != "https://glucava.example.com" {
		t.Fatalf("public_url = %q", got)
	}
	// A value saved in the UI wins over a later env change.
	t.Setenv("GLUCAVA_PUBLIC_URL", "https://other.example.com")
	if err := EnsurePublicURL(app); err != nil {
		t.Fatal(err)
	}
	if got := smtpRec(t, app).GetString("public_url"); got != "https://glucava.example.com" {
		t.Errorf("seed overwrote the stored value: %q", got)
	}
}
