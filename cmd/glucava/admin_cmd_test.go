package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func run(t *testing.T, app core.App, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := userCommand(app)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestUserCommands(t *testing.T) {
	app := newTestStore(t).App
	users, _ := app.FindCollectionByNameOrId("users")
	u := core.NewRecord(users)
	u.SetEmail("typo@example.test")
	u.SetPassword("original-password")
	if err := app.Save(u); err != nil {
		t.Fatal(err)
	}

	if out, err := run(t, app, "", "show"); err != nil || strings.TrimSpace(out) != "typo@example.test" {
		t.Fatalf("show = %q, %v", out, err)
	}
	if _, err := run(t, app, "", "set-email", "not-an-email"); err == nil {
		t.Error("an invalid email was accepted")
	}
	if _, err := run(t, app, "", "set-email", "right@example.test"); err != nil {
		t.Fatalf("set-email without the old address: %v", err)
	}
	if _, err := run(t, app, "a-new-password!\n", "set-password"); err != nil {
		t.Fatalf("set-password without an email: %v", err)
	}
	rec, err := app.FindAuthRecordByEmail("users", "right@example.test")
	if err != nil || !rec.ValidatePassword("a-new-password!") {
		t.Fatalf("account not updated: %v", err)
	}
	if _, err := run(t, app, "short\n", "set-password"); err == nil {
		t.Error("a short password was accepted")
	}
	if _, err := run(t, app, "another-password!\n", "set-password", "nobody@example.test"); err == nil {
		t.Error("an unknown email was accepted")
	}

	u2 := core.NewRecord(users)
	u2.SetEmail("second@example.test")
	u2.SetPassword("second-password!")
	if err := app.Save(u2); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, app, "", "set-email", "x@example.test"); err == nil {
		t.Error("with two users the email must be named")
	}
}
