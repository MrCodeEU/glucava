// Package testutil builds PocketBase apps for tests quickly. Creating a
// database and running every migration costs seconds under -race, so a
// Template does it once per test binary and hands each test a file copy.
package testutil

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// Template is a migrated (and optionally seeded) data directory that tests
// clone. The importing package must register the migrations it needs.
type Template struct {
	prep func(core.App) error
	once sync.Once
	dir  string
	err  error
}

// NewTemplate returns a Template; prep runs once after migrations and may be nil.
func NewTemplate(prep func(core.App) error) *Template { return &Template{prep: prep} }

func (tp *Template) build() {
	dir, err := os.MkdirTemp("", "glucava-template-*")
	if err != nil {
		tp.err = err
		return
	}
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	if tp.err = app.Bootstrap(); tp.err == nil {
		if tp.err = app.RunAllMigrations(); tp.err == nil && tp.prep != nil {
			tp.err = tp.prep(app)
		}
	}
	// Closing flushes the WAL into the main database files so a plain copy is complete.
	_ = app.ClearBootstrap()
	tp.dir = dir
}

// App returns a fresh app whose data directory is a copy of the template.
func (tp *Template) App(t *testing.T) core.App {
	t.Helper()
	tp.once.Do(tp.build)
	if tp.err != nil {
		t.Fatal(tp.err)
	}
	dst := t.TempDir()
	entries, err := os.ReadDir(tp.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(tp.dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dst})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	return app
}
