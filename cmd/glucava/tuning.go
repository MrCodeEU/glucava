package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/strava"
)

// tuning holds the operational knobs that are not user settings: timings and
// Strava page selectors. They come from the environment, are read once at
// start, and every one has the default that used to be hardcoded. A bad value
// stops the server with a clear message instead of being silently ignored.
type tuning struct {
	CanaryInterval time.Duration // 0 disables the canary
	PollLookback   time.Duration // how old an activity may be and still be picked up by polling
	RetryBackoff   []time.Duration
	NotifyCooldown time.Duration // one notification per event type and activity per period
	NotifyMaxAge   time.Duration // undeliverable events older than this are dropped
	UserAgent      string        // empty means the Writer's built-in one
	Selectors      strava.Selectors
}

// loadTuning reads the environment through getenv (os.Getenv in production).
//
//	GLUCAVA_CANARY_INTERVAL     duration, or "off"                 (default 24h)
//	GLUCAVA_POLL_LOOKBACK       duration                           (default 24h)
//	GLUCAVA_RETRY_BACKOFF       comma-separated durations          (default 1m,3m,10m)
//	GLUCAVA_NOTIFY_COOLDOWN     duration                           (default 6h)
//	GLUCAVA_NOTIFY_MAX_AGE      duration                           (default 24h)
//	GLUCAVA_USER_AGENT          browser user agent string          (default built in)
//	GLUCAVA_STRAVA_SELECTOR_DESCRIPTION, GLUCAVA_STRAVA_SELECTOR_SAVE
//	                            JSON array of CSS selectors, tried before the built-in ones
func loadTuning(getenv func(string) string) (tuning, error) {
	t := tuning{
		CanaryInterval: 24 * time.Hour,
		PollLookback:   24 * time.Hour,
		RetryBackoff:   jobs.DefaultBackoff,
		NotifyCooldown: 6 * time.Hour,
		NotifyMaxAge:   24 * time.Hour,
		UserAgent:      getenv("GLUCAVA_USER_AGENT"),
		Selectors: strava.Selectors{
			Description: append([]string(nil), strava.DefaultSelectors.Description...),
			Save:        append([]string(nil), strava.DefaultSelectors.Save...),
		},
	}

	for _, d := range []struct {
		name string
		dst  *time.Duration
		off  bool // "off" is allowed and means 0
	}{
		{"GLUCAVA_CANARY_INTERVAL", &t.CanaryInterval, true},
		{"GLUCAVA_POLL_LOOKBACK", &t.PollLookback, false},
		{"GLUCAVA_NOTIFY_COOLDOWN", &t.NotifyCooldown, false},
		{"GLUCAVA_NOTIFY_MAX_AGE", &t.NotifyMaxAge, false},
	} {
		raw := strings.TrimSpace(getenv(d.name))
		if raw == "" {
			continue
		}
		if d.off && strings.EqualFold(raw, "off") {
			*d.dst = 0
			continue
		}
		v, err := time.ParseDuration(raw)
		if err != nil || v <= 0 {
			return t, fmt.Errorf("%s must be a positive duration like 90m or 24h, got %q", d.name, raw)
		}
		*d.dst = v
	}

	if raw := strings.TrimSpace(getenv("GLUCAVA_RETRY_BACKOFF")); raw != "" {
		var out []time.Duration
		for _, part := range strings.Split(raw, ",") {
			v, err := time.ParseDuration(strings.TrimSpace(part))
			if err != nil || v <= 0 {
				return t, fmt.Errorf("GLUCAVA_RETRY_BACKOFF must be comma-separated positive durations like 1m,3m,10m, got %q", raw)
			}
			out = append(out, v)
		}
		t.RetryBackoff = out
	}

	for _, s := range []struct {
		name string
		dst  *[]string
	}{
		{"GLUCAVA_STRAVA_SELECTOR_DESCRIPTION", &t.Selectors.Description},
		{"GLUCAVA_STRAVA_SELECTOR_SAVE", &t.Selectors.Save},
	} {
		raw := strings.TrimSpace(getenv(s.name))
		if raw == "" {
			continue
		}
		var extra []string
		if err := json.Unmarshal([]byte(raw), &extra); err != nil {
			return t, fmt.Errorf(`%s must be a JSON array of CSS selectors, e.g. ["textarea.notes"]: %w`, s.name, err)
		}
		for _, sel := range extra {
			if strings.TrimSpace(sel) == "" {
				return t, fmt.Errorf("%s contains an empty selector", s.name)
			}
		}
		*s.dst = append(extra, *s.dst...) // overrides first, built-ins stay as fallback
	}
	return t, nil
}
