package web

import (
	"strings"

	"github.com/MrCodeEU/glucava/internal/i18n"
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
}

// eventTypeLabel is the human name of an event type. A type without a key (a
// new one that was not translated yet) shows its raw name with spaces.
func eventTypeLabel(tr *i18n.Translator, typ string) string {
	if k, ok := eventTypeKeys[typ]; ok {
		return tr.T(k) // i18n:dynamic (keys registered in eventTypeKeys)
	}
	return strings.ReplaceAll(typ, "_", " ")
}
