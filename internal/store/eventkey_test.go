package store

import (
	"context"
	"testing"

	"github.com/MrCodeEU/glucava/internal/jobs"
)

// The message key and its arguments survive the events collection, both on the
// notification outbox and on the log page; a row without them reads back empty.
func TestEventMessageKeyRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	if err := s.RecordEvent(ctx, jobs.Event{
		Type: jobs.EventGlucoseGap, Severity: "warning", Message: "english",
		MsgKey: "event.gap", MsgArgs: map[string]any{"age_min": 125, "last_at": "2026-09-30T05:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordEvent(ctx, jobs.Event{Type: jobs.EventStravaFailed, Severity: "error", Message: "plain"}); err != nil {
		t.Fatal(err)
	}

	pending, err := s.Pending(ctx)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending = %v, %v", pending, err)
	}
	var keyed, plain = pending[0], pending[1]
	if keyed.MsgKey != "event.gap" || keyed.MsgArgs["age_min"] != float64(125) || keyed.MsgArgs["last_at"] != "2026-09-30T05:00:00Z" {
		t.Errorf("outbox event = %+v", keyed)
	}
	if plain.MsgKey != "" || plain.MsgArgs != nil || plain.Message != "plain" {
		t.Errorf("event without a key = %+v", plain)
	}

	rows, err := s.ListEvents(ctx, 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %v, %v", rows, err)
	}
	for _, r := range rows {
		if r.Type == jobs.EventGlucoseGap && (r.MsgKey != "event.gap" || r.MsgArgs["age_min"] != float64(125)) {
			t.Errorf("row = %+v", r)
		}
	}
}
