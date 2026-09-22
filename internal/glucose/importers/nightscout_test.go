package importers

import (
	"strings"
	"testing"
	"time"
)

const nightscoutFixture = `[
  {"_id":"a1","type":"sgv","sgv":112,"date":1695800000000,"dateString":"2023-09-27T05:33:20.000Z"},
  {"_id":"a2","type":"sgv","sgv":108,"date":1695800300000,"dateString":"2023-09-27T05:38:20.000Z"},
  {"_id":"a3","type":"mbg","sgv":110,"date":1695800600000},
  {"_id":"a4","type":"cal","date":1695800900000}
]`

func TestNightscoutParse(t *testing.T) {
	imp, ok := Get("nightscout")
	if !ok {
		t.Fatal("nightscout importer not registered")
	}
	samples, skipped, err := imp.Parse(strings.NewReader(nightscoutFixture))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0 (mbg/cal are intentionally not glucose readings)", skipped)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(samples))
	}
	if samples[0].Value != 112 || !samples[0].Time.Equal(time.UnixMilli(1695800000000)) {
		t.Errorf("sample[0] = %+v", samples[0])
	}
	if samples[1].Value != 108 {
		t.Errorf("sample[1] = %+v", samples[1])
	}
}

func TestNightscoutParseInvalidJSON(t *testing.T) {
	imp, _ := Get("nightscout")
	if _, _, err := imp.Parse(strings.NewReader("not json")); err == nil {
		t.Error("invalid JSON accepted")
	}
}

func TestNightscoutParseSkipsBadEntries(t *testing.T) {
	imp, _ := Get("nightscout")
	samples, skipped, err := imp.Parse(strings.NewReader(`[{"type":"sgv","sgv":0,"date":1695800000000}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 0 || skipped != 1 {
		t.Errorf("samples=%d skipped=%d, want 0/1", len(samples), skipped)
	}
}
