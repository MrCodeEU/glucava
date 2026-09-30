package eventmsg

import (
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

var (
	en = i18n.English()
	de = i18n.Default().For("de")
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	if Encode(nil) != "" || Encode(map[string]any{}) != "" || Decode("") != nil || Decode("not json") != nil {
		t.Error("empty input must give empty output")
	}
	back := Decode(Encode(map[string]any{"age_min": 125, "ip": "203.0.113.7"}))
	if back["ip"] != "203.0.113.7" || back["age_min"] != float64(125) {
		t.Errorf("round trip = %v", back)
	}
}

func TestRenderFormatsNeutralArgsForTheLocale(t *testing.T) {
	args := Decode(Encode(map[string]any{"age_min": 125, "last_at": "2026-09-30T05:00:00Z"}))
	utc := time.UTC
	if got := Render(en, utc, KeyGap, args, "x"); got != "No glucose reading for 2h 05m (the last one was at Wed 30 Sep, 05:00)." {
		t.Errorf("en = %q", got)
	}
	if got := Render(de, utc, KeyGap, args, "x"); got != "Seit 2 h 05 min kein Glukosewert (der letzte war am Mi, 30. Sep, 05:00)." {
		t.Errorf("de = %q", got)
	}
	vienna, _ := time.LoadLocation("Europe/Vienna")
	if got := Render(en, vienna, KeyGap, args, "x"); !strings.Contains(got, "07:00") {
		t.Errorf("time not shown in the zone: %q", got)
	}
	short := Render(en, utc, KeyGap, map[string]any{"age_min": 25, "last_at": "2026-09-30T05:00:00Z"}, "x")
	if !strings.Contains(short, "for 25m ") {
		t.Errorf("minutes only = %q", short)
	}
}

func TestRenderFallsBackToTheStoredMessage(t *testing.T) {
	for name, key := range map[string]string{"no key": "", "unknown key": "event.from.the.future"} {
		if got := Render(de, time.UTC, key, nil, "stored english"); got != "stored english" {
			t.Errorf("%s: %q", name, got)
		}
	}
	if got := Render(nil, time.UTC, KeyGap, nil, "stored english"); got != "stored english" {
		t.Errorf("nil translator: %q", got)
	}
}

func TestEveryKeyHasAMessageInBothLanguages(t *testing.T) {
	for _, k := range []string{KeyGap, KeyCanaryOpenFailed, KeyCanaryLogin, KeyCanaryNoField, KeyCanaryNoSave, KeyCanaryNotFound,
		KeyActivityFailed, KeyRestoreFailed, KeyPollSession, KeyTriggerMissing, KeyTriggerInvalid} {
		for _, tr := range []*i18n.Translator{en, de} {
			got := Render(tr, time.UTC, k, map[string]any{"id": "7", "err": "boom", "ip": "1.2.3.4", "age_min": 5, "last_at": "2026-09-30T05:00:00Z"}, "FALLBACK")
			if got == "FALLBACK" || strings.Contains(got, "event.") || strings.Contains(got, "{") {
				t.Errorf("%s %s = %q", tr.Tag(), k, got)
			}
		}
	}
	if got := Render(de, time.UTC, KeyTriggerInvalid, map[string]any{"ip": "1.2.3.4"}, ""); got != "Auslöser-Aufruf von 1.2.3.4 abgelehnt: ungültiges Token." {
		t.Errorf("de trigger = %q", got)
	}
}
