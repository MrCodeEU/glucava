package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepository is the CI gate: every key used in Go code exists in en.json,
// no key is dynamic without a marker, locales have no extra keys or broken
// placeholders, and en.json has no unused keys. Untranslated keys are only
// warnings. Run it with `make i18n-check`.
func TestRepository(t *testing.T) {
	rep, err := Run("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range rep.Warnings {
		t.Log("warning:", w)
	}
	for _, e := range rep.Errors {
		t.Error(e)
	}
}

func TestScanFindsProblems(t *testing.T) {
	dir := t.TempDir()
	src := `package x

func f(tr Tr, pd PD, k string) {
	_ = tr.T("nav.activities")
	_ = tr.T("no.such.key")
	_ = tr.T(k)
	_ = tr.T(k) // i18n:dynamic
	_ = t(pd, "nav.settings")
	_ = tn(pd, "items", 2)
	_ = t(pd)
}
`
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	uses, errs, err := scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, u := range uses {
		keys = append(keys, u.key)
	}
	if got := strings.Join(keys, ","); got != "nav.activities,no.such.key,nav.settings,items" {
		t.Errorf("keys = %s", got)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "x.go:6") {
		t.Errorf("errors = %v", errs)
	}
	if !uses[3].plural {
		t.Error("tn key should be plural")
	}
}

func TestPlaceholders(t *testing.T) {
	got := placeholders("a {x} b {n} {x}")
	if len(got) != 2 || !got["x"] || !got["n"] {
		t.Errorf("placeholders = %v", got)
	}
}
