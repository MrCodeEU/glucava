package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/store"
)

// pushSignals are the signals the Push notifications card sends. PushSub is
// the browser's PushSubscription as JSON text (subscribe) or just the
// endpoint (unsubscribe), both produced by static/pwa.js.
type pushSignals struct {
	PushSub string `json:"pushSub"`
}

// pushDevices lists the subscribed devices, logging (not failing) on error.
func (s *Server) pushDevices(ctx context.Context) []notify.PushSub {
	subs, err := s.Store.PushSubscriptions(ctx)
	if err != nil {
		slog.Error("push: list devices", "err", err)
	}
	return subs
}

// patchPushDevices re-renders the device list in place.
func (s *Server) patchPushDevices(sse *datastar.ServerSentEventGenerator, r *http.Request) {
	_ = sse.PatchElements(renderString(PushDevices(s.pushDevices(r.Context()), s.loc(), s.now())))
}

func (s *Server) actionPushSubscribe(w http.ResponseWriter, r *http.Request) {
	var v pushSignals
	readErr := datastar.ReadSignals(r, &v)
	sse := datastar.NewSSE(w, r)
	if readErr != nil {
		s.toast(sse, "error", "Could not read the subscription.")
		return
	}
	var sub struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(v.PushSub), &sub); err != nil {
		s.toast(sse, "error", "The browser sent an unreadable subscription.")
		return
	}
	if err := notify.ValidPushEndpoint(sub.Endpoint); err != nil {
		s.toast(sse, "error", "Not subscribed: "+err.Error()+".")
		return
	}
	if err := notify.ValidPushKeys(sub.Keys.P256dh, sub.Keys.Auth); err != nil {
		s.toast(sse, "error", "Not subscribed: "+err.Error()+".")
		return
	}
	err := s.Store.UpsertPushSubscription(r.Context(), notify.PushSub{
		Endpoint: sub.Endpoint, P256dh: sub.Keys.P256dh, Auth: sub.Keys.Auth, UserAgent: r.UserAgent(),
	})
	if err != nil {
		s.toast(sse, "error", "Could not save the device: "+err.Error())
		return
	}
	s.patchPushDevices(sse, r)
	s.toast(sse, "ok", "This device will now get push notifications.")
}

func (s *Server) actionPushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var v pushSignals
	readErr := datastar.ReadSignals(r, &v)
	sse := datastar.NewSSE(w, r)
	if readErr != nil {
		s.toast(sse, "error", "Could not read the request.")
		return
	}
	if ep := strings.TrimSpace(v.PushSub); ep != "" {
		if err := s.Store.DeletePushSubscription(r.Context(), ep); err != nil {
			s.toast(sse, "error", "Could not remove the device: "+err.Error())
			return
		}
	}
	s.patchPushDevices(sse, r)
	s.toast(sse, "ok", "Push notifications are off on this device.")
}

func (s *Server) actionPushRemove(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if err := s.Store.DeletePushSubscriptionID(r.Context(), r.PathValue("id")); err != nil {
		s.toast(sse, "error", "Could not remove the device: "+err.Error())
		return
	}
	s.patchPushDevices(sse, r)
	s.toast(sse, "ok", "Device removed.")
}

func (s *Server) actionPushTest(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		s.toast(sse, "error", err.Error())
		return
	}
	ch, err := notify.NewWebPush(s.Store, s.Vault, cfg.PublicURL, cfg.EmailTo, s.Demo)
	if err != nil {
		s.toast(sse, "error", "Push is not available: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	msg := notify.Message{Type: notify.TypeTest, Severity: "info", Title: "glucava test", Body: "Push notifications work on this device.", Time: s.now()}
	if err := ch.Send(ctx, msg); err != nil {
		s.toast(sse, "error", "Test failed: "+err.Error())
	} else {
		s.toast(sse, "ok", "Test push sent.")
	}
	s.patchPushDevices(sse, r) // last success / error may have changed
}

// pushCardData gathers what the settings card shows. The VAPID key is
// generated here on first use; it is never shown, only its public half.
func (s *Server) pushCardData(ctx context.Context, _ store.Config) PushCardData {
	d := PushCardData{Devices: s.pushDevices(ctx), Loc: s.loc(), Now: s.now()}
	if _, pub, err := notify.EnsureVAPID(s.Vault); err != nil {
		slog.Error("push: VAPID key", "err", err)
	} else {
		d.VAPIDPublic = pub
	}
	return d
}

// pushRoutes mounts the push actions (session auth and same-origin check
// come from page()).
func (s *Server) pushRoutes(page func(string, http.HandlerFunc)) {
	page("POST /actions/push/subscribe", s.actionPushSubscribe)
	page("POST /actions/push/unsubscribe", s.actionPushUnsubscribe)
	page("POST /actions/push/test", s.actionPushTest)
	page("POST /actions/push/remove/{id}", s.actionPushRemove)
}
