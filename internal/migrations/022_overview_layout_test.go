package migrations

import (
	"encoding/json"
	"testing"
)

func TestLayoutFromToggles(t *testing.T) {
	var cards []struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	raw := layoutFromToggles(false, true, false, true, false)
	if err := json.Unmarshal([]byte(raw), &cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != len(overviewLayoutOrder) {
		t.Fatalf("got %d cards, want %d", len(cards), len(overviewLayoutOrder))
	}
	want := map[string]bool{
		"trend": false, "by_sport": true, "table": false, "kpis": true, "sources": false,
		"tir": true, "agp": true, "heatmap": true, "calendar": true, "dayparts": true, "episodes": true, "insights": true,
	}
	for i, c := range cards {
		if c.ID != overviewLayoutOrder[i] {
			t.Errorf("card %d = %s, want %s", i, c.ID, overviewLayoutOrder[i])
		}
		if c.Enabled != want[c.ID] {
			t.Errorf("%s enabled = %v, want %v", c.ID, c.Enabled, want[c.ID])
		}
	}
}
