package importers

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOpenZipMemberFindsMatch(t *testing.T) {
	data := makeZip(t, map[string]string{"a.txt": "no", "target.csv": "yes"})
	content, name, err := openZipMember(data, func(n string) bool { return strings.HasSuffix(n, ".csv") })
	if err != nil {
		t.Fatal(err)
	}
	if name != "target.csv" || string(content) != "yes" {
		t.Errorf("name=%q content=%q", name, content)
	}
}

func TestOpenZipMemberNoMatch(t *testing.T) {
	data := makeZip(t, map[string]string{"a.txt": "no"})
	if _, _, err := openZipMember(data, func(n string) bool { return strings.HasSuffix(n, ".csv") }); err == nil {
		t.Error("no matching entry: expected an error")
	}
}

// TestOpenZipMemberNeverTouchesDisk documents the zip-slip defense: a
// traversal entry name is only ever compared as a string, never joined into
// a filesystem path, so this succeeds (returns the content in memory)
// without writing anything, no matter how hostile the name is.
func TestOpenZipMemberTraversalNameIsJustAString(t *testing.T) {
	evilName := "../../../../tmp/should-never-be-written.csv"
	data := makeZip(t, map[string]string{evilName: "payload"})
	content, name, err := openZipMember(data, func(n string) bool { return n == evilName })
	if err != nil {
		t.Fatal(err)
	}
	if name != evilName || string(content) != "payload" {
		t.Errorf("name=%q content=%q", name, content)
	}
}

func TestLooksLikeZip(t *testing.T) {
	if !looksLikeZip(makeZip(t, map[string]string{"a": "b"})) {
		t.Error("real zip not detected")
	}
	if looksLikeZip([]byte("not a zip")) {
		t.Error("plain text detected as zip")
	}
}
