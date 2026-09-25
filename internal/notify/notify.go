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

	Icon      string // emoji shown in the email header; empty picks one from Type
	Chart     []byte // PNG shown in the email, if any
	ChartAlt  string
	Facts     []Fact // label/value rows, e.g. the numbers in a summary
	Link      string // web UI page for this message; empty when no public URL is set
	LinkLabel string
}

// Fact is one label/value row in a message.
type Fact struct{ Label, Value string }

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
