// Package notify delivers events from the outbox to ntfy, webhooks and similar channels.
package notify

import (
	"context"
	"time"
)

// Message is one notification.
type Message struct {
	Type     string
	Severity string // info, warning, error
	Title    string
	Body     string
	StravaID string
	Repaired bool
	Time     time.Time
}

// Channel sends a Message somewhere.
type Channel interface {
	Name() string
	Send(ctx context.Context, m Message) error
}

// titles maps event types to short human titles.
var titles = map[string]string{
	"strava_failed":       "Strava update failed",
	"selector_repaired":   "Strava selector repaired",
	"session_expired":     "Strava session expired",
	"glucose_unavailable": "Glucose data unavailable",
	"canary_failed":       "Strava canary check failed",
	"trigger_rejected":    "Trigger call rejected",
}

// Title returns the title for an event type.
func Title(eventType string) string {
	if t, ok := titles[eventType]; ok {
		return t
	}
	return eventType
}
