package importers

import (
	"strings"
	"testing"
	"time"
)

// libreFixture approximates a published LibreView export layout: a title row,
// then a header row, then historic (auto) and scan (manual) rows, plus a
// non-glucose (insulin) row sharing the same column count. Not verified
// against a real Abbott export; see the UNVERIFIED note on libreImporter.
const libreFixture = "Glucose Data,Generated on 2026-09-01\r\n" +
	"Device,Serial Number,Device Timestamp,Record Type,Historic Glucose mg/dL,Scan Glucose mg/dL,Notes\r\n" +
	"FreeStyle LibreLink,ABC123,01-09-2026 08:00,0,110,,\r\n" +
	"FreeStyle LibreLink,ABC123,01-09-2026 08:15,0,118,,\r\n" +
	"FreeStyle LibreLink,ABC123,01-09-2026 08:20,1,,120,\r\n" +
	"FreeStyle LibreLink,ABC123,01-09-2026 09:00,5,,,woke up\r\n"

func TestLibreParse(t *testing.T) {
	imp, ok := Get("libre")
	if !ok {
		t.Fatal("libre importer not registered")
	}
	samples, skipped, err := imp.Parse(strings.NewReader(libreFixture))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if len(samples) != 3 {
		t.Fatalf("samples = %d, want 3: %+v", len(samples), samples)
	}
	want := time.Date(2026, 9, 1, 8, 0, 0, 0, time.Local)
	if !samples[0].Time.Equal(want) || samples[0].Value != 110 {
		t.Errorf("sample[0] = %+v, want time=%v value=110", samples[0], want)
	}
	if samples[2].Value != 120 {
		t.Errorf("sample[2] (scan) = %+v, want value=120", samples[2])
	}
}

func TestLibreParseMmolConverts(t *testing.T) {
	fixture := "Glucose Data\r\n" +
		"Device Timestamp,Historic Glucose mmol/L\r\n" +
		"01-09-2026 08:00,6.1\r\n"
	imp, _ := Get("libre")
	samples, _, err := imp.Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(samples))
	}
	got := samples[0].Value
	if got < 109 || got > 111 { // 6.1 mmol/L * 18.016 ~= 109.9
		t.Errorf("value = %v, want ~110", got)
	}
}

func TestLibreParseNoHeaderErrors(t *testing.T) {
	imp, _ := Get("libre")
	if _, _, err := imp.Parse(strings.NewReader("just,some,csv\r\n1,2,3\r\n")); err == nil {
		t.Error("missing header accepted")
	}
}
