package tokens

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	_ "github.com/pocketbase/pocketbase/migrations" // registers the system migrations

	_ "github.com/MrCodeEU/glucava/internal/migrations"
)

func newManager(t *testing.T) (*Manager, core.App) {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	return &Manager{App: app}, app
}

func TestCreateVerifyRevoke(t *testing.T) {
	m, app := newManager(t)

	tok, err := m.Create("tasker")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, prefix) || len(tok) < 40 {
		t.Fatalf("token = %q", tok)
	}

	// Only the hash is stored.
	rec, _ := app.FindFirstRecordByData("api_tokens", "name", "tasker")
	if strings.Contains(rec.GetString("token_hash"), tok) || rec.GetString("token_hash") != hash(tok) {
		t.Error("stored value is not the token hash")
	}

	if ok, err := m.Verify(tok); !ok || err != nil {
		t.Fatalf("verify valid = %v, %v", ok, err)
	}
	if list, _ := m.List(); len(list) != 1 || list[0].LastUsed.IsZero() {
		t.Errorf("list = %+v, want last_used set", list)
	}

	for _, bad := range []string{"", "gst_nope", "not-a-token", tok + "x"} {
		if ok, _ := m.Verify(bad); ok {
			t.Errorf("verify(%q) = true", bad)
		}
	}

	if err := m.Revoke("tasker"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := m.Verify(tok); ok {
		t.Error("revoked token still verifies")
	}
}

func TestCreateValidation(t *testing.T) {
	m, _ := newManager(t)
	if _, err := m.Create("  "); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := m.Create("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create("a"); err == nil {
		t.Error("duplicate name accepted")
	}
	if err := m.Revoke("missing"); err == nil {
		t.Error("revoking unknown name should fail")
	}
}

func TestTokensAreUnique(t *testing.T) {
	m, _ := newManager(t)
	a, _ := m.Create("a")
	b, _ := m.Create("b")
	if a == b {
		t.Fatal("tokens collide")
	}
}

func TestScopes(t *testing.T) {
	m, app := newManager(t)

	legacy, _ := m.Create("legacy") // no scopes asked for
	backupOnly, err := m.Create("cron", ScopeBackup)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		token string
		scope Scope
		want  bool
	}{
		{"default token fires the trigger", legacy, ScopeTrigger, true},
		{"default token reads metrics", legacy, ScopeMetrics, true},
		{"default token cannot back up", legacy, ScopeBackup, false},
		{"backup token backs up", backupOnly, ScopeBackup, true},
		{"backup token cannot fire the trigger", backupOnly, ScopeTrigger, false},
		{"backup token cannot read metrics", backupOnly, ScopeMetrics, false},
	} {
		if got, err := m.VerifyScope(c.token, c.scope); err != nil || got != c.want {
			t.Errorf("%s: got %v, %v", c.name, got, err)
		}
	}
	if ok, _ := m.Verify(backupOnly); ok {
		t.Error("Verify (trigger) accepted a backup token")
	}

	// A record with an unreadable scope list grants nothing.
	rec, _ := app.FindFirstRecordByData("api_tokens", "name", "cron")
	rec.Set("scopes", "garbage")
	if err := app.Save(rec); err != nil {
		t.Fatal(err)
	}
	if ok, _ := m.VerifyScope(backupOnly, ScopeBackup); ok {
		t.Error("garbage scopes still grant backup")
	}

	if _, err := ParseScopes("trigger,nope"); err == nil {
		t.Error("unknown scope accepted")
	}
	if got, _ := ParseScopes(""); len(got) != 2 {
		t.Errorf("empty list = %v, want the defaults", got)
	}
}
