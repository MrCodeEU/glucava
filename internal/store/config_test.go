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
		case "smtp_tls", "mail_alerts", "mail_activity", "mail_weekly", "mail_health", "chart_image", "chart_band", "chart_activity", "chart_dots", "chart_hr", "hr_read":
			v = "true"
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
		"unit":       func(c *Config) { c.Unit = "x" },
		"range":      func(c *Config) { c.RangeHigh = 60 },
		"poll":       func(c *Config) { c.PollMin = 0 },
		"retention":  func(c *Config) { c.RetentionDays = -1 },
		"region":     func(c *Config) { c.DexcomRegion = "mars" },
		"ntfy":       func(c *Config) { c.NtfyURL = "javascript:alert(1)" },
		"email":      func(c *Config) { c.EmailTo = "nope" },
		"smtp port":  func(c *Config) { c.SMTPPort = 70000 },
		"smtp host":  func(c *Config) { c.SMTPHost = "h" },
		"smtp addr":  func(c *Config) { c.SMTPSender = "nope" },
		"pre window": func(c *Config) { c.PreMin = 999 },
		"public url": func(c *Config) { c.PublicURL = "ftp://x" },
		"gap alert":  func(c *Config) { c.GapAlertHours = 500 },
	}
	for name, mut := range bad {
		c := valid()
		mut(&c)
		if c.Validate() == "" {
			t.Errorf("%s: not rejected", name)
		}
	}
}
