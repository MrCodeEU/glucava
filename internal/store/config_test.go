package store

import (
	"strings"
	"testing"
)

func TestConfigGetSetRoundTripEveryKey(t *testing.T) {
	var c Config
	for _, k := range ConfigKeys() {
		v := "1"
		switch k {
		case "unit":
			v = "mmol/L"
		case "dexcom_region":
			v = "us"
		case "smtp_tls", "mail_alerts", "mail_activity", "mail_weekly", "mail_health", "chart_image", "chart_band", "chart_activity", "chart_dots", "chart_hr", "chart_elevation", "hr_read",
			"chart_avg_line", "chart_range_lines", "chart_min_max", "chart_hide_stats":
			v = "true"
		case "overview_default_range":
			v = "7d"
		case "overview_layout":
			v = `[{"id":"kpis","enabled":false}]`
		case "chart_theme":
			v = "dark"
		case "chart_size":
			v = "large"
		case "ntfy_url", "webhook_url", "public_url":
			v = "https://x.example/y"
		case "email_to", "smtp_sender_address":
			v = "a@example.com"
		case "dexcom_username", "smtp_host", "smtp_username", "smtp_sender_name":
			v = "text"
		}
		if err := c.Set(k, v); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
		if got, _ := c.Get(k); got != v {
			t.Errorf("%s: got %q, want %q", k, got, v)
		}
	}
}

func TestConfigSetRejectsBadInput(t *testing.T) {
	var c Config
	for k, v := range map[string]string{"nope": "1", "range_low": "abc", "pre_minutes": "1.5", "smtp_tls": "maybe"} {
		if err := c.Set(k, v); err == nil {
			t.Errorf("Set(%s, %q) accepted", k, v)
		}
	}
	if _, err := c.Get("nope"); err == nil || !strings.Contains(err.Error(), "known:") {
		t.Errorf("Get unknown: %v", err)
	}
}

func valid() Config {
	return Config{Unit: "mg/dL", RangeLow: 70, RangeHigh: 180, PollMin: 10, DexcomRegion: "ous", ChartTheme: "light", ChartSize: "standard", ChartLine: 2, ChartPreMin: 30}
}

func TestConfigValidate(t *testing.T) {
	if msg := valid().Validate(); msg != "" {
		t.Fatalf("valid config rejected: %s", msg)
	}
	bad := map[string]func(*Config){
		"unit":        func(c *Config) { c.Unit = "x" },
		"range":       func(c *Config) { c.RangeHigh = 60 },
		"poll":        func(c *Config) { c.PollMin = 0 },
		"retention":   func(c *Config) { c.RetentionDays = -1 },
		"region":      func(c *Config) { c.DexcomRegion = "mars" },
		"ntfy":        func(c *Config) { c.NtfyURL = "javascript:alert(1)" },
		"email":       func(c *Config) { c.EmailTo = "nope" },
		"smtp port":   func(c *Config) { c.SMTPPort = 70000 },
		"smtp host":   func(c *Config) { c.SMTPHost = "h" },
		"smtp addr":   func(c *Config) { c.SMTPSender = "nope" },
		"pre window":  func(c *Config) { c.PreMin = 999 },
		"public url":  func(c *Config) { c.PublicURL = "ftp://x" },
		"gap alert":   func(c *Config) { c.GapAlertHours = 500 },
		"post buffer": func(c *Config) { c.PostBufferMin = 999 },
		"description template": func(c *Config) {
			c.DescriptionTemplate = "{{.NoSuchField}}"
		},
		"chart panel order": func(c *Config) { c.ChartPanelOrder = "nope" },
	}
	for name, mut := range bad {
		c := valid()
		mut(&c)
		if c.Validate() == "" {
			t.Errorf("%s: not rejected", name)
		}
	}
}

// TestChartStylePanelOrder covers Config.ChartStyle's Panels construction:
// only enabled panels appear, in ChartPanelOrder's order, and a panel
// that's enabled but missing from (or misspelled in) the order string still
// shows up, appended at the end.
func TestChartStylePanelOrder(t *testing.T) {
	c := Config{ChartBand: true, ChartActivity: true, ChartDots: true, ChartHR: false, ChartPanelOrder: "dots,activity,band"}
	got := c.ChartStyle().Panels
	want := []string{"dots", "activity", "band"}
	if fmtStrs(got) != fmtStrs(want) {
		t.Errorf("Panels = %v, want %v", got, want)
	}

	// hr is enabled but not mentioned in the order string.
	c2 := Config{ChartActivity: true, ChartHR: true, ChartPanelOrder: "activity"}
	got2 := c2.ChartStyle().Panels
	want2 := []string{"activity", "hr"}
	if fmtStrs(got2) != fmtStrs(want2) {
		t.Errorf("Panels = %v, want %v", got2, want2)
	}

	// nothing enabled: Panels must be an explicit empty slice, not nil
	// (nil would mean chartimg's own default, i.e. everything shown).
	c3 := Config{}
	if p := c3.ChartStyle().Panels; p == nil || len(p) != 0 {
		t.Errorf("Panels = %v, want a non-nil empty slice", p)
	}
}

func fmtStrs(s []string) string { return strings.Join(s, ",") }

func TestValidChartPanelOrder(t *testing.T) {
	for _, ok := range []string{"", "activity", "activity,band,dots,hr", " band , hr "} {
		if !ValidChartPanelOrder(ok) {
			t.Errorf("ValidChartPanelOrder(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"nope", "activity,nope", "Activity"} {
		if ValidChartPanelOrder(bad) {
			t.Errorf("ValidChartPanelOrder(%q) = true, want false", bad)
		}
	}
}

// TestConfigValidateAcceptsAValidDescriptionTemplate is the flip side of the
// "description template" bad-input case above: a template that actually
// parses and executes must not be rejected just for existing.
func TestConfigValidateAcceptsAValidDescriptionTemplate(t *testing.T) {
	c := valid()
	c.DescriptionTemplate = "TIR {{.TIR}}%"
	if msg := c.Validate(); msg != "" {
		t.Errorf("valid description template rejected: %s", msg)
	}
}

func TestConfigValidateThresholds(t *testing.T) {
	ok := func(vl, vh float64) bool {
		c := valid()
		c.VeryLow, c.VeryHigh = vl, vh
		return c.Validate() == ""
	}
	for _, tc := range []struct {
		vl, vh float64
		want   bool
	}{
		{0, 0, true}, {54, 250, true}, {60, 200, true}, {69, 181, true},
		{70, 250, false}, {19, 250, false}, {54, 180, false}, {54, 601, false}, {54, 600, true},
	} {
		if got := ok(tc.vl, tc.vh); got != tc.want {
			t.Errorf("very low %v / very high %v: valid = %v, want %v", tc.vl, tc.vh, got, tc.want)
		}
	}
}

func TestConfigRangeCarriesThresholds(t *testing.T) {
	c := valid()
	c.VeryLow, c.VeryHigh = 60, 240
	r := c.Range()
	if vl, vh := r.Thresholds(); r.Low != 70 || r.High != 180 || vl != 60 || vh != 240 {
		t.Errorf("range = %+v (%v, %v)", r, vl, vh)
	}
	if vl, vh := valid().Range().Thresholds(); vl != 54 || vh != 250 {
		t.Errorf("zero thresholds resolve to %v/%v, want 54/250", vl, vh)
	}
}
