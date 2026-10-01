package web

import (
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/eventmsg"
	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/store"
)

// eventTypeKeys maps a jobs event type to its translation key. Keep it in step
// with the event constants in internal/jobs/types.go.
var eventTypeKeys = map[string]string{
	"strava_failed":       i18n.Key("events.type.strava_failed"),
	"session_expired":     i18n.Key("events.type.session_expired"),
	"glucose_unavailable": i18n.Key("events.type.glucose_unavailable"),
	"canary_failed":       i18n.Key("events.type.canary_failed"),
	"glucose_gap":         i18n.Key("events.type.glucose_gap"),
	"activity_not_found":  i18n.Key("events.type.activity_not_found"),
	"selector_repaired":   i18n.Key("events.type.selector_repaired"),
	"trigger_rejected":    i18n.Key("events.type.trigger_rejected"),
}

// eventTypeLabel is the human name of an event type. A type without a key (a
// new one that was not translated yet) shows its raw name with spaces.
func eventTypeLabel(tr *i18n.Translator, typ string) string {
	if k, ok := eventTypeKeys[typ]; ok {
		return tr.T(k) // i18n:dynamic (keys registered in eventTypeKeys)
	}
	return strings.ReplaceAll(typ, "_", " ")
}

// eventMessage is the message of a stored event in the reader's language. A
// row without a message key (written by an older version) shows the English
// text it was stored with.
func eventMessage(tr *i18n.Translator, loc *time.Location, e store.EventRow) string {
	return eventmsg.Render(tr, loc, e.MsgKey, e.MsgArgs, e.Message)
}
