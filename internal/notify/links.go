package notify

import (
	"strings"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

// Message types that are not failures. Summaries are sent to email only.
const (
	TypeTest            = "test"
	TypeActivitySummary = "activity_summary"
	TypeWeeklySummary   = "weekly_summary"
	TypeHealthReport    = "health_report"
)

// IsAlert reports whether a message type is a failure alert (as opposed to a
// test or a summary).
func IsAlert(msgType string) bool {
	switch msgType {
	case TypeTest, TypeActivitySummary, TypeWeeklySummary, TypeHealthReport:
		return false
	}
	return true
}

// LinkFor returns the web UI page that best matches a message, and a label
// for it. base is the public URL of the UI; with none configured there is no
// link, so nothing points at a made-up address.
func LinkFor(tr *i18n.Translator, base string, m Message) (href, label string) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", ""
	}
	switch m.Type {
	case TypeWeeklySummary, "glucose_gap":
		return base + "/", tr.T("notify.link.dashboard")
	case TypeHealthReport:
		return base + "/events", tr.T("notify.link.events")
	case "session_expired", "canary_failed":
		return base + "/strava", tr.T("notify.link.strava")
	case "trigger_rejected":
		return base + "/tokens", tr.T("notify.link.tokens")
	case TypeTest:
		return base + "/settings", tr.T("notify.link.settings")
	}
	if m.StravaID != "" {
		return base + "/activity/" + m.StravaID, tr.T("notify.link.activity")
	}
	return base + "/events", tr.T("notify.link.events")
}
