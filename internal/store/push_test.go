package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/notify"
)

func TestPushSubscriptionLifecycle(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()

	a := notify.PushSub{Endpoint: "https://push.example/a", P256dh: "k1", Auth: "s1", UserAgent: "Firefox"}
	if err := s.UpsertPushSubscription(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPushSubscription(ctx, notify.PushSub{Endpoint: "https://push.example/b", P256dh: "k2", Auth: "s2"}); err != nil {
		t.Fatal(err)
	}
	// Same endpoint again: keys replaced, no second row.
	a.P256dh = "k1-new"
	if err := s.UpsertPushSubscription(ctx, a); err != nil {
		t.Fatal(err)
	}
	subs, err := s.PushSubscriptions(ctx)
	if err != nil || len(subs) != 2 {
		t.Fatalf("subs = %+v, %v", subs, err)
	}
	if subs[0].Endpoint != a.Endpoint || subs[0].P256dh != "k1-new" || subs[0].UserAgent != "Firefox" {
		t.Errorf("first = %+v", subs[0])
	}
	if !subs[0].LastOK.IsZero() || subs[0].LastError != "" {
		t.Errorf("new device has history: %+v", subs[0])
	}

	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	if err := s.PushResult(ctx, a.Endpoint, at, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.PushResult(ctx, "https://push.example/b", at, "HTTP 500"); err != nil {
		t.Fatal(err)
	}
	if err := s.PushResult(ctx, "https://push.example/gone", at, ""); err != nil {
		t.Errorf("unknown endpoint: %v", err)
	}
	subs, _ = s.PushSubscriptions(ctx)
	if !subs[0].LastOK.Equal(at) || subs[0].LastError != "" {
		t.Errorf("after success: %+v", subs[0])
	}
	if subs[1].LastError != "HTTP 500" || !subs[1].LastOK.IsZero() {
		t.Errorf("after failure: %+v", subs[1])
	}

	if err := s.DeletePushSubscription(ctx, "https://push.example/b"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePushSubscriptionID(ctx, subs[0].ID); err != nil {
		t.Fatal(err)
	}
	if subs, _ = s.PushSubscriptions(ctx); len(subs) != 0 {
		t.Errorf("left over: %+v", subs)
	}
	if err := s.DeletePushSubscription(ctx, "https://push.example/none"); err != nil {
		t.Errorf("deleting unknown: %v", err)
	}
}

func TestPushSubscriptionLimit(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	for i := range maxPushSubscriptions {
		if err := s.UpsertPushSubscription(ctx, notify.PushSub{Endpoint: fmt.Sprintf("https://push.example/%d", i), P256dh: "k", Auth: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	err := s.UpsertPushSubscription(ctx, notify.PushSub{Endpoint: "https://push.example/extra", P256dh: "k", Auth: "a"})
	if !errors.Is(err, ErrTooManyDevices) {
		t.Errorf("over limit: %v", err)
	}
	// Renewing an existing one still works at the limit.
	if err := s.UpsertPushSubscription(ctx, notify.PushSub{Endpoint: "https://push.example/0", P256dh: "k2", Auth: "a"}); err != nil {
		t.Errorf("renew at limit: %v", err)
	}
}

func TestPushConfigDefaults(t *testing.T) {
	s := &PB{App: newApp(t)}
	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PushAlerts || cfg.PushSummaries {
		t.Errorf("defaults alerts=%v summaries=%v, want true/false", cfg.PushAlerts, cfg.PushSummaries)
	}
	cfg.PushAlerts, cfg.PushSummaries = false, true
	if err := s.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	back, _ := s.LoadConfig()
	if back.PushAlerts || !back.PushSummaries {
		t.Errorf("round trip = %v/%v", back.PushAlerts, back.PushSummaries)
	}
}
