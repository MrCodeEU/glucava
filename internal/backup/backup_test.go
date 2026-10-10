package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	_ "github.com/pocketbase/pocketbase/migrations" // registers the system migrations

	_ "github.com/MrCodeEU/glucava/internal/migrations"
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

func entries(t *testing.T, b []byte) map[string]bool {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, f := range zr.File {
		out[f.Name] = true
	}
	return out
}

func TestKeyOnlyWhenAsked(t *testing.T) {
	app := newApp(t)
	if err := os.WriteFile(filepath.Join(app.DataDir(), keyName), []byte("KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	var plain, withKey bytes.Buffer
	if err := Create(context.Background(), app, &plain, "test", false); err != nil {
		t.Fatal(err)
	}
	if err := Create(context.Background(), app, &withKey, "test", true); err != nil {
		t.Fatal(err)
	}
	if e := entries(t, plain.Bytes()); e[keyName] || !e[dbName] || !e[manifestName] {
		t.Errorf("default backup entries = %v, want database and manifest but no key", e)
	}
	if e := entries(t, withKey.Bytes()); !e[keyName] {
		t.Errorf("--include-key backup entries = %v, want the key", e)
	}
	// The snapshot leaves no temp directory behind.
	left, _ := filepath.Glob(filepath.Join(app.DataDir(), "backup-*"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestRoundTripAndForce(t *testing.T) {
	app := newApp(t)
	if _, err := app.DB().NewQuery("INSERT INTO api_tokens (id, name, token_hash, scopes) VALUES ('abc123abc123abc', 'marker', 'h', 'backup')").Execute(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Create(context.Background(), app, &buf, "1.2.3", false); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "b.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	m, err := Restore(zipPath, dst, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Build != "1.2.3" || m.IncludesKey {
		t.Errorf("manifest = %+v", m)
	}
	// The restored file opens as a real database holding the marker row.
	restored := core.NewBaseApp(core.BaseAppConfig{DataDir: dst})
	if err := restored.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restored.ClearBootstrap() }()
	if rec, err := restored.FindFirstRecordByData("api_tokens", "name", "marker"); err != nil || rec.GetString("scopes") != "backup" {
		t.Fatalf("marker row missing after restore: %v", err)
	}

	if _, err := Restore(zipPath, dst, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("restore over an existing database = %v, want a --force hint", err)
	}
	if _, err := Restore(zipPath, dst, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, dbName+".before-restore")); err != nil {
		t.Errorf("old database was not kept: %v", err)
	}
}

func TestRestoreRejectsForeignZips(t *testing.T) {
	dir := t.TempDir()
	notZip := filepath.Join(dir, "x.zip")
	_ = os.WriteFile(notZip, []byte("nope"), 0o600)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../../evil")
	_, _ = w.Write([]byte("x"))
	_ = zw.Close()
	foreign := filepath.Join(dir, "y.zip")
	_ = os.WriteFile(foreign, buf.Bytes(), 0o600)

	for _, p := range []string{notZip, foreign, filepath.Join(dir, "missing.zip")} {
		dst := filepath.Join(dir, "out")
		if _, err := Restore(p, dst, false); err == nil {
			t.Errorf("Restore(%s) succeeded", p)
		}
		if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
			t.Error("a zip entry escaped the data dir")
		}
	}
}
