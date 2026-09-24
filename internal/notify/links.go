package notify

import "strings"

// Message types that are not failures. Summaries are sent to email only.
const (
	TypeTest            = "test"
	TypeActivitySummary = "activity_summary"
	TypeWeeklySummary   = "weekly_summary"
)

// IsAlert reports whether a message type is a failure alert (as opposed to a
// test or a summary).
func IsAlert(msgType string) bool {
	switch msgType {
	case TypeTest, TypeActivitySummary, TypeWeeklySummary:
		return false
	}
	return true
}

// LinkFor returns the web UI page that best matches a message, and a label
// for it. base is the public URL of the UI; with none configured there is no
// link, so nothing points at a made-up address.
func LinkFor(base string, m Message) (href, label string) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", ""
	}
	switch m.Type {
	case TypeWeeklySummary:
		return base + "/", "Open dashboard"
	case "session_expired", "canary_failed":
		return base + "/strava", "Check the Strava connection"
	case "trigger_rejected":
		return base + "/tokens", "Manage trigger tokens"
	case TypeTest:
		return base + "/settings", "Open settings"
	}
	if m.StravaID != "" {
		return base + "/activity/" + m.StravaID, "Open in glucava"
	}
	return base + "/events", "See all events"
}
