package main

import (
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/strava"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadTuningDefaults(t *testing.T) {
	got, err := loadTuning(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.CanaryInterval != 24*time.Hour || got.PollLookback != 24*time.Hour ||
		got.NotifyCooldown != 6*time.Hour || got.NotifyMaxAge != 24*time.Hour || len(got.RetryBackoff) != 3 {
		t.Errorf("defaults = %+v", got)
	}
	if len(got.Selectors.Description) != len(strava.DefaultSelectors.Description) {
		t.Errorf("default selectors changed: %v", got.Selectors.Description)
	}
}

func TestLoadTuningOverrides(t *testing.T) {
	got, err := loadTuning(env(map[string]string{
		"GLUCAVA_CANARY_INTERVAL":             "off",
		"GLUCAVA_POLL_LOOKBACK":               "72h",
		"GLUCAVA_RETRY_BACKOFF":               "10s, 1m",
		"GLUCAVA_NOTIFY_COOLDOWN":             "30m",
		"GLUCAVA_USER_AGENT":                  "UA/1",
		"GLUCAVA_STRAVA_SELECTOR_DESCRIPTION": `["textarea.notes"]`,
		"GLUCAVA_STRAVA_SELECTOR_SAVE":        `["button.save"]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.CanaryInterval != 0 || got.PollLookback != 72*time.Hour || got.NotifyCooldown != 30*time.Minute || got.UserAgent != "UA/1" {
		t.Errorf("got %+v", got)
	}
	if len(got.RetryBackoff) != 2 || got.RetryBackoff[0] != 10*time.Second {
		t.Errorf("backoff = %v", got.RetryBackoff)
	}
	// Overrides come first, built-ins remain as fallback.
	if got.Selectors.Description[0] != "textarea.notes" || len(got.Selectors.Description) != len(strava.DefaultSelectors.Description)+1 {
		t.Errorf("description selectors = %v", got.Selectors.Description)
	}
	if got.Selectors.Save[0] != "button.save" {
		t.Errorf("save selectors = %v", got.Selectors.Save)
	}
	if strava.DefaultSelectors.Save[0] == "button.save" {
		t.Error("override leaked into the shared defaults")
	}
}

func TestLoadTuningRejectsBadValues(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"duration":       {"GLUCAVA_POLL_LOOKBACK": "soon"},
		"zero":           {"GLUCAVA_NOTIFY_MAX_AGE": "0s"},
		"off elsewhere":  {"GLUCAVA_POLL_LOOKBACK": "off"},
		"backoff":        {"GLUCAVA_RETRY_BACKOFF": "1m,x"},
		"selector json":  {"GLUCAVA_STRAVA_SELECTOR_SAVE": "button.save"},
		"empty selector": {"GLUCAVA_STRAVA_SELECTOR_DESCRIPTION": `[" "]`},
	} {
		if _, err := loadTuning(env(m)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestNewSource(t *testing.T) {
	got, err := newSource("", nil, nil)
	if err != nil || got.name != "dexcom" || got.source == nil {
		t.Errorf("default = %+v, %v", got, err)
	}
	if _, err := newSource("carrier-pigeon", nil, nil); err == nil || !strings.Contains(err.Error(), "known: dexcom") {
		t.Errorf("unknown source: %v", err)
	}
}
