package store

import "testing"

func TestNormalizeOverviewLayout(t *testing.T) {
	got := NormalizeOverviewLayout([]OverviewCard{
		{ID: "table", Enabled: false},
		{ID: "nope", Enabled: true},
		{ID: "table", Enabled: true}, // repeat dropped
		{ID: "heatmap", Enabled: true, Options: map[string]string{"metric": "tir", "bogus": "x"}},
		{ID: "agp", Enabled: true, Options: map[string]string{"hours": "invalid"}},
	})
	if len(got) != len(OverviewCards) {
		t.Fatalf("len = %d, want %d (missing cards appended)", len(got), len(OverviewCards))
	}
	if got[0].ID != "table" || got[0].Enabled {
		t.Errorf("first = %+v, want table disabled", got[0])
	}
	if got[1].ID != "heatmap" || got[1].Options["metric"] != "tir" || len(got[1].Options) != 1 {
		t.Errorf("heatmap options = %+v", got[1])
	}
	if got[2].ID != "agp" || got[2].Options != nil {
		t.Errorf("agp with invalid option = %+v, want options dropped", got[2])
	}
	seen := map[string]bool{}
	for _, c := range got {
		if c.ID == "nope" || seen[c.ID] {
			t.Errorf("bad card kept: %+v", c)
		}
		seen[c.ID] = true
	}
	// A card appended because it was missing is enabled.
	for _, c := range got[3:] {
		if !c.Enabled {
			t.Errorf("appended card %s not enabled", c.ID)
		}
	}
}

func TestParseOverviewLayout(t *testing.T) {
	if cards, err := ParseOverviewLayout(""); err != nil || len(cards) != len(OverviewCards) {
		t.Errorf("empty = %d cards, %v", len(cards), err)
	}
	if _, err := ParseOverviewLayout("{not json"); err == nil {
		t.Error("invalid JSON accepted")
	}
	c := Config{OverviewLayout: "garbage"}
	if len(c.OverviewCards()) != len(OverviewCards) {
		t.Error("garbage layout must fall back to the default")
	}
}

func TestOverviewRangeAndValidate(t *testing.T) {
	c := valid()
	c.OverviewDefaultRange = "14d"
	if msg := c.Validate(); msg != "" || c.OverviewRange() != "14d" {
		t.Errorf("14d: %q %q", msg, c.OverviewRange())
	}
	c.OverviewDefaultRange = "5y"
	if c.Validate() == "" {
		t.Error("5y accepted")
	}
	if c.OverviewRange() != DefaultOverviewRange {
		t.Errorf("invalid range fell back to %q", c.OverviewRange())
	}
	c.OverviewDefaultRange = ""
	c.OverviewLayout = "[oops"
	if c.Validate() == "" {
		t.Error("broken layout JSON accepted")
	}
}
