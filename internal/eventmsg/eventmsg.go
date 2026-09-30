// Package eventmsg renders the message of a stored event in a language.
//
// An event is written by a background job, long before anyone reads it, and it
// is read by people who may use different languages (the Notifications page
// follows the browser, a push or email follows the installation setting). So
// the job stores a translation key with its arguments next to the English
// message, and the reader renders them when the text is needed. Rows without a
// key (written before the key existed) keep their English message.
//
// Arguments are stored neutral, not formatted: a name ending in "_min" holds a
// whole number of minutes and "_at" an RFC 3339 time. Render formats them for
// the locale and the placeholder is the name without the suffix ("age_min"
// fills {age}).
package eventmsg

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

// The message keys the jobs record. i18n.Key marks each as used, so the key
// check verifies them against the locale files.
var (
	KeyGap              = i18n.Key("event.gap")
	KeyCanaryOpenFailed = i18n.Key("event.canary.open_failed")
	KeyCanaryLogin      = i18n.Key("event.canary.login")
	KeyCanaryNoField    = i18n.Key("event.canary.no_field")
	KeyCanaryNoSave     = i18n.Key("event.canary.no_save")
	KeyCanaryNotFound   = i18n.Key("event.canary.not_found")
	KeyActivityFailed   = i18n.Key("event.activity.failed")
	KeyRestoreFailed    = i18n.Key("event.activity.restore_failed")
	KeyPollSession      = i18n.Key("event.poll.session_expired")
	KeyTriggerMissing   = i18n.Key("event.trigger.missing_token")
	KeyTriggerInvalid   = i18n.Key("event.trigger.invalid_token")
)

// Encode stores args as the JSON text kept with the event; nil and empty maps
// are the empty string.
func Encode(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return string(b)
}

// Decode is the inverse of Encode. Bad or empty input gives nil.
func Decode(s string) map[string]any {
	if s == "" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil {
		return nil
	}
	return m
}

// Render returns the message for key with args, formatted for tr and shown in
// loc. It returns fallback (the stored English message) when there is no key or
// the translator does not know it, so an old row or a key from a newer build
// never renders as a raw key name.
func Render(tr *i18n.Translator, loc *time.Location, key string, args map[string]any, fallback string) string {
	if key == "" || tr == nil || !tr.Has(key) {
		return fallback
	}
	if loc == nil {
		loc = time.UTC
	}
	var flat []any
	for name, v := range args {
		switch {
		case strings.HasSuffix(name, "_min"):
			flat = append(flat, strings.TrimSuffix(name, "_min"), duration(tr, number(v)))
		case strings.HasSuffix(name, "_at"):
			s, _ := v.(string)
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				flat = append(flat, strings.TrimSuffix(name, "_at"), s)
				continue
			}
			flat = append(flat, strings.TrimSuffix(name, "_at"), tr.When(t, loc, time.Now()))
		default:
			flat = append(flat, name, plain(v))
		}
	}
	return tr.T(key, flat...) // i18n:dynamic (keys are registered where the events are recorded)
}

// duration formats whole minutes as "25m" or "1h 05m" in the locale's words.
func duration(tr *i18n.Translator, mins int) string {
	if mins < 60 {
		return tr.T("event.dur.min", "m", strconv.Itoa(mins))
	}
	return tr.T("event.dur.hm", "h", strconv.Itoa(mins/60), "m", pad2(mins%60))
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// number reads a JSON number (float64 after decoding) or a Go int.
func number(v any) int {
	switch x := v.(type) {
	case float64:
		return int(math.Round(x))
	case int:
		return x
	case int64:
		return int(x)
	}
	return 0
}

// plain turns an argument into text; whole numbers lose the ".0" JSON adds.
func plain(v any) any {
	if f, ok := v.(float64); ok && f == math.Trunc(f) {
		return strconv.FormatInt(int64(f), 10)
	}
	return v
}
