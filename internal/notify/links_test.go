package notify

import "testing"

func TestLinkFor(t *testing.T) {
	tests := []struct {
		m    Message
		base string
		want string
	}{
		{Message{Type: "strava_failed", StravaID: "42"}, "https://g.example", "https://g.example/activity/42"},
		{Message{Type: "glucose_unavailable", StravaID: "7"}, "https://g.example/", "https://g.example/activity/7"}, // trailing slash trimmed
		{Message{Type: "session_expired"}, "https://g.example", "https://g.example/strava"},
		{Message{Type: "canary_failed", StravaID: "42"}, "https://g.example", "https://g.example/strava"},
		{Message{Type: "trigger_rejected"}, "https://g.example", "https://g.example/tokens"},
		{Message{Type: TypeTest}, "https://g.example", "https://g.example/settings"},
		{Message{Type: TypeWeeklySummary}, "https://g.example", "https://g.example/"},
		{Message{Type: TypeActivitySummary, StravaID: "9"}, "https://g.example", "https://g.example/activity/9"},
		{Message{Type: "strava_failed"}, "https://g.example", "https://g.example/events"},
		{Message{Type: "glucose_gap"}, "https://g.example", "https://g.example/"},
		{Message{Type: TypeHealthReport}, "https://g.example", "https://g.example/events"},
		{Message{Type: "strava_failed", StravaID: "42"}, "", ""},
		{Message{Type: "strava_failed", StravaID: "42"}, "  ", ""},
	}
	for _, tc := range tests {
		got, label := LinkFor(tc.base, tc.m)
		if got != tc.want || (got != "") != (label != "") {
			t.Errorf("LinkFor(%q, %s) = %q, %q; want %q", tc.base, tc.m.Type, got, label, tc.want)
		}
	}
}

func TestIsAlert(t *testing.T) {
	for _, typ := range []string{"strava_failed", "session_expired", "canary_failed", "trigger_rejected", "glucose_gap"} {
		if !IsAlert(typ) {
			t.Errorf("%s should be an alert", typ)
		}
	}
	for _, typ := range []string{TypeTest, TypeActivitySummary, TypeWeeklySummary, TypeHealthReport} {
		if IsAlert(typ) {
			t.Errorf("%s is not an alert", typ)
		}
	}
}
