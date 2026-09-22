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
