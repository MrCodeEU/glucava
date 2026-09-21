package secrets

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	_ "github.com/MrCodeEU/glucava/internal/migrations"
	_ "github.com/pocketbase/pocketbase/migrations"
)

func newVault(t *testing.T) *Vault {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	return &Vault{App: app, Cipher: testCipher(t)}
}

func TestReencryptMovesEverythingToTheNewKey(t *testing.T) {
	v := newVault(t)
	for name, val := range map[string]string{NameDexcomPassword: "pw", NameStravaCookies: "cookies"} {
		if err := v.Set(name, val); err != nil {
			t.Fatal(err)
		}
	}
	next, err := NewCipher([]byte(strings.Repeat("n", keyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Reencrypt(next); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Get(NameDexcomPassword); err == nil {
		t.Error("old key still opens the value")
	}
	v.Cipher = next
	if got, ok, err := v.Get(NameStravaCookies); err != nil || !ok || got != "cookies" {
		t.Errorf("Get = %q, %v, %v", got, ok, err)
	}
}

func TestReencryptIsAtomicWhenOneValueIsUnreadable(t *testing.T) {
	v := newVault(t)
	_ = v.Set(NameDexcomPassword, "pw")
	_ = v.Set(NameStravaCookies, "cookies")
	rec, _ := v.App.FindFirstRecordByData("secrets", "name", NameStravaCookies)
	rec.Set("ciphertext", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err := v.App.Save(rec); err != nil {
		t.Fatal(err)
	}
	next, _ := NewCipher([]byte(strings.Repeat("n", keyBytes)))
	if err := v.Reencrypt(next); err == nil {
		t.Fatal("expected an error")
	}
	if got, _, err := v.Get(NameDexcomPassword); err != nil || got != "pw" {
		t.Errorf("first value changed after a failed rotation: %q, %v", got, err)
	}
}
