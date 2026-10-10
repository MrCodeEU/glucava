package store

import (
	"slices"
	"testing"
)

func TestSourceSettingsAreScriptableAndValidated(t *testing.T) {
	var c Config
	if err := c.Set("source_nightscout_url", "https://ns.example"); err != nil {
		t.Fatal(err)
	}
	if v, err := c.Get("source_nightscout_url"); err != nil || v != "https://ns.example" {
		t.Errorf("get = %q, %v", v, err)
	}
	if err := c.Set("dexcom_region", "us"); err != nil || c.DexcomRegion != "us" {
		t.Errorf("legacy key: %v %q", err, c.DexcomRegion)
	}
	for _, k := range []string{"glucose_source", "source_nightscout_url", "source_librelinkup_region", "source_librelinkup_email", "dexcom_region"} {
		if !slices.Contains(ConfigKeys(), k) {
			t.Errorf("ConfigKeys lacks %s", k)
		}
	}
	// A secret field is never a setting.
	if slices.Contains(ConfigKeys(), "source_nightscout_token") {
		t.Error("a secret is exposed as a config key")
	}

	base := Config{Unit: "mg/dL", RangeLow: 70, RangeHigh: 180, PollMin: 10, ChartTheme: "light", ChartSize: "standard", ChartLine: 2, DexcomRegion: "ous"}
	if msg := base.Validate(); msg != "" {
		t.Fatalf("base config invalid: %s", msg)
	}
	bad := base
	_ = bad.Set("glucose_source", "carrier-pigeon")
	if bad.Validate() == "" {
		t.Error("unknown source accepted")
	}
	bad = base
	_ = bad.Set("source_nightscout_url", "ftp://nope")
	if bad.Validate() == "" {
		t.Error("bad nightscout url accepted")
	}
	bad = base
	bad.DexcomRegion = "mars"
	if bad.Validate() == "" {
		t.Error("bad dexcom region accepted")
	}

	// Setting on a copy leaves the original's map alone.
	a := base
	_ = a.Set("source_nightscout_url", "https://a.example")
	b := a
	_ = b.Set("source_nightscout_url", "https://b.example")
	if v, _ := a.Get("source_nightscout_url"); v != "https://a.example" {
		t.Errorf("copy changed the original: %q", v)
	}
}

func TestSourceSettingsRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	c, _ := s.LoadConfig()
	c.GlucoseSource = "nightscout"
	_ = c.Set("source_nightscout_url", "https://ns.example")
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadConfig()
	if v, _ := got.Get("source_nightscout_url"); got.GlucoseSource != "nightscout" || v != "https://ns.example" {
		t.Errorf("after reload: %q %q", got.GlucoseSource, v)
	}
}
