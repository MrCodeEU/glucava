package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	_ "github.com/MrCodeEU/glucava/internal/migrations"
	"github.com/MrCodeEU/glucava/internal/store"
	_ "github.com/pocketbase/pocketbase/migrations"
)

func newTestStore(t *testing.T) *store.PB {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	return &store.PB{App: app}
}

func TestParsePairs(t *testing.T) {
	got, err := parsePairs([]string{`unit=mmol/L`, ` smtp_host = "smtp.example.com" `, `webhook_url=https://a/b?x=1`})
	if err != nil {
		t.Fatal(err)
	}
	want := []pair{{"unit", "mmol/L"}, {"smtp_host", "smtp.example.com"}, {"webhook_url", "https://a/b?x=1"}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pair %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, bad := range []string{"novalue", "=x"} {
		if _, err := parsePairs([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestApplyConfigIsIdempotentAndAtomic(t *testing.T) {
	st := newTestStore(t)
	var out bytes.Buffer

	if err := applyConfig(&out, st, []pair{{"unit", "mmol/L"}, {"poll_interval_minutes", "5"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "changed unit: mg/dL -> mmol/L") {
		t.Errorf("out = %q", out.String())
	}
	cfg, _ := st.LoadConfig()
	if cfg.Unit != "mmol/L" || cfg.PollMin != 5 {
		t.Errorf("cfg = %+v", cfg)
	}

	out.Reset()
	if err := applyConfig(&out, st, []pair{{"unit", "mmol/L"}}); err != nil || strings.TrimSpace(out.String()) != "unchanged" {
		t.Errorf("second run: %v %q", err, out.String())
	}

	// One bad value in the batch: nothing is saved, not even the valid pair.
	if err := applyConfig(&out, st, []pair{{"unit", "mg/dL"}, {"poll_interval_minutes", "0"}}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if cfg, _ := st.LoadConfig(); cfg.Unit != "mmol/L" {
		t.Errorf("partial save: %+v", cfg)
	}

	// Related keys are validated together, in any order.
	if err := applyConfig(&out, st, []pair{{"smtp_host", "smtp.example.com"}, {"smtp_port", "587"}, {"smtp_sender_address", "a@example.com"}}); err != nil {
		t.Errorf("smtp batch: %v", err)
	}
	if err := applyConfig(&out, st, []pair{{"nope", "1"}}); err == nil {
		t.Error("unknown key accepted")
	}
}
