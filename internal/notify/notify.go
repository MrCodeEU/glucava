// Package notify delivers events from the outbox to ntfy, webhooks and similar channels.
package notify

import (
	"context"
	"time"

	"github.com/MrCodeEU/glucava/internal/i18n"
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

// Translator returns the translator for texts that are made without a
// request (alerts, summaries, the test message). The server passes one that
// follows the installation language setting at call time, so a change applies
// to the next message. A nil function, or one that returns nil, means English.
// notify stays free of the store: it only ever sees this function.
type Translator func() *i18n.Translator

// Get returns the translator to use now; never nil.
func (f Translator) Get() *i18n.Translator {
	if f != nil {
		if tr := f(); tr != nil {
			return tr
		}
	}
	return i18n.English()
}

// titles maps event types to the translation key of their short title.
var titles = map[string]string{
	"strava_failed":       i18n.Key("notify.title.strava_failed"),
	"selector_repaired":   i18n.Key("notify.title.selector_repaired"),
	"session_expired":     i18n.Key("notify.title.session_expired"),
	"glucose_unavailable": i18n.Key("notify.title.glucose_unavailable"),
	"canary_failed":       i18n.Key("notify.title.canary_failed"),
	"glucose_gap":         i18n.Key("notify.title.glucose_gap"),
	"activity_not_found":  i18n.Key("notify.title.activity_not_found"),
	"trigger_rejected":    i18n.Key("notify.title.trigger_rejected"),
	TypeTest:              i18n.Key("notify.title.test"),
}

// Title returns the title for an event type in the language of tr. A type
// without a title shows its name.
func Title(tr *i18n.Translator, eventType string) string {
	if k, ok := titles[eventType]; ok {
		return tr.T(k) // i18n:dynamic (keys registered in titles)
	}
	return eventType
}
