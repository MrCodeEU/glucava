package importers

import (
	"bytes"
	"strings"
	"testing"
)

func TestNamesListsBuiltins(t *testing.T) {
	names := Names()
	want := map[string]bool{"libre": true, "nightscout": true, "glooko": true}
	for _, n := range names {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("missing from Names(): %v (got %v)", want, names)
	}
}

func TestGetUnknown(t *testing.T) {
	if _, ok := Get("does-not-exist"); ok {
		t.Error("unknown importer returned ok=true")
	}
}

func TestCheckZipSupportRejectsZipForNonZipAwareFormat(t *testing.T) {
	nightscout, _ := Get("nightscout")
	zipData := makeZip(t, map[string]string{"a.json": "[]"})
	r := bytes.NewReader(zipData)

	err := CheckZipSupport(nightscout, r, "nightscout")
	if err == nil {
		t.Fatal("zip accepted for a format that isn't ZipAware")
	}
	if !strings.Contains(err.Error(), "nightscout") {
		t.Errorf("error doesn't name the format: %v", err)
	}
	// It must rewind r so the caller can still read the file normally.
	if pos, _ := r.Seek(0, 1); pos != 0 {
		t.Errorf("reader not rewound: at %d", pos)
	}
}

func TestCheckZipSupportAllowsZipForZipAwareFormat(t *testing.T) {
	glooko, _ := Get("glooko")
	zipData := makeZip(t, map[string]string{"cgm_data_1.csv": "x"})
	if err := CheckZipSupport(glooko, bytes.NewReader(zipData), "glooko"); err != nil {
		t.Errorf("zip rejected for glooko: %v", err)
	}
}

func TestCheckZipSupportIgnoresNonZip(t *testing.T) {
	nightscout, _ := Get("nightscout")
	if err := CheckZipSupport(nightscout, strings.NewReader("[]"), "nightscout"); err != nil {
		t.Errorf("non-zip content rejected: %v", err)
	}
}
