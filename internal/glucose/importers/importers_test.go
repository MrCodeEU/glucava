package importers

import "testing"

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
