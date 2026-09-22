package importers

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
)

// glookoFixture matches the real export's layout (title row, BOM, German
// column names, comma-decimal values, dot-separated dates) but with
// synthetic data, not a real person's readings.
const glookoFixture = "\xEF\xBB\xBFName:Test User,Datumsbereich:01.01.2026 - 02.01.2026\r\n" +
	"Zeitstempel,CGM-Glukosewert (mg/dl),Seriennummer\r\n" +
	"01.01.2026 08:00,\"110,0\",Dexcom G7\r\n" +
	"01.01.2026 08:05,\"112,5\",Dexcom G7\r\n"

func TestGlookoParseCSV(t *testing.T) {
	imp, ok := Get("glooko")
	if !ok {
		t.Fatal("glooko importer not registered")
	}
	samples, skipped, err := imp.Parse(strings.NewReader(glookoFixture))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2: %+v", len(samples), samples)
	}
	want := time.Date(2026, 1, 1, 8, 0, 0, 0, time.Local)
	if !samples[0].Time.Equal(want) || samples[0].Value != 110 {
		t.Errorf("sample[0] = %+v, want time=%v value=110", samples[0], want)
	}
	if samples[1].Value != 112.5 {
		t.Errorf("sample[1] = %+v, want value=112.5", samples[1])
	}
}

func TestGlookoParseZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// A realistic export bundles several files; only cgm_data_*.csv matters.
	for _, name := range []string{"bg_data_1.csv", "Manual data/notes_data_1.csv", "cgm_data_1.csv"} {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		content := "not glucose data\n"
		if name == "cgm_data_1.csv" {
			content = glookoFixture
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	imp, _ := Get("glooko")
	samples, _, err := imp.Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2 (bg_data/notes should be ignored)", len(samples))
	}
}

func TestGlookoParseZipNoMatchingEntry(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, _ := zw.Create("bg_data_1.csv")
	_, _ = fw.Write([]byte("not cgm data"))
	_ = zw.Close()

	imp, _ := Get("glooko")
	if _, _, err := imp.Parse(&buf); err == nil {
		t.Error("zip with no cgm_data entry accepted")
	}
}

func TestGlookoParseNoHeaderErrors(t *testing.T) {
	imp, _ := Get("glooko")
	if _, _, err := imp.Parse(strings.NewReader("just,some,csv\r\n1,2,3\r\n")); err == nil {
		t.Error("missing header accepted")
	}
}
