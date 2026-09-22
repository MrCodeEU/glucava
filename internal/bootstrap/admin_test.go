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

func TestSilenceSuperuserPromptIgnoresTheInstallerAccount(t *testing.T) {
	// Reproduces an existing data dir where only PocketBase's own one-time
	// installer superuser exists (core.DefaultInstallerEmail): counting all
	// superusers would see it and wrongly skip creating a real one.
	app := newApp(t)
	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail(core.DefaultInstallerEmail)
	rec.SetPassword("a-long-enough-password")
	if err := app.Save(rec); err != nil {
		t.Fatal(err)
	}

	if err := SilenceSuperuserPrompt(app); err != nil {
		t.Fatal(err)
	}
	// PocketBase deletes its own installer placeholder as soon as a real
	// superuser exists (core/record_model_superusers.go), so only ours remains.
	n, err := app.CountRecords(core.CollectionNameSuperusers)
	if err != nil || n != 1 {
		t.Fatalf("superusers = %d, %v, want 1", n, err)
	}
	if rec, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, core.DefaultInstallerEmail); err == nil {
		t.Errorf("installer placeholder still present: %s", rec.Id)
	}
}
