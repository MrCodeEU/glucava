package web

import (
	"context"
	"strings"
	"testing"

	"github.com/MrCodeEU/glucava/internal/eventmsg"
	"github.com/MrCodeEU/glucava/internal/jobs"
)

// A stored event renders from its message key in the reader's language, and an
// old row without a key keeps the English text it was stored with.
func TestEventMessagesRenderInTheReadersLanguage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	ctx := context.Background()
	if err := e.srv.Store.RecordEvent(ctx, jobs.Event{
		Type: "trigger_rejected", Severity: "warning", Message: "trigger call from 203.0.113.7 rejected: invalid token",
		MsgKey: eventmsg.KeyTriggerInvalid, MsgArgs: map[string]any{"ip": "203.0.113.7"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.srv.Store.RecordEvent(ctx, jobs.Event{Type: "strava_failed", Severity: "error", Message: "old row, no key"}); err != nil {
		t.Fatal(err)
	}

	de := getWithLang(e, "/events", "de", c).Body.String()
	if !strings.Contains(de, "Auslöser-Aufruf von 203.0.113.7 abgelehnt: ungültiges Token.") || !strings.Contains(de, "old row, no key") {
		t.Errorf("German events page: %s", de)
	}
	en := getWithLang(e, "/events", "en", c).Body.String()
	if !strings.Contains(en, "Trigger call from 203.0.113.7 rejected: invalid token.") || strings.Contains(en, "event.trigger") {
		t.Errorf("English events page: %s", en)
	}
}
