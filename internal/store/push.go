package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/MrCodeEU/glucava/internal/notify"
)

// maxPushSubscriptions bounds how many devices can register, so a stray
// client cannot fill the table.
const maxPushSubscriptions = 20

// ErrTooManyDevices is returned by UpsertPushSubscription at the limit.
var ErrTooManyDevices = errors.New("store: too many push devices; remove one first")

func pushFromRecord(r *core.Record) notify.PushSub {
	return notify.PushSub{
		ID: r.Id, Endpoint: r.GetString("endpoint"), P256dh: r.GetString("p256dh"), Auth: r.GetString("auth"),
		UserAgent: r.GetString("user_agent"), Created: r.GetDateTime("created").Time(),
		LastOK: r.GetDateTime("last_ok").Time(), LastError: r.GetString("last_error"),
	}
}

// PushSubscriptions returns every subscribed device, oldest first.
func (s *PB) PushSubscriptions(_ context.Context) ([]notify.PushSub, error) {
	recs, err := s.App.FindAllRecords("push_subscriptions")
	if err != nil {
		return nil, err
	}
	out := make([]notify.PushSub, 0, len(recs))
	for _, r := range recs {
		out = append(out, pushFromRecord(r))
	}
	slices.SortStableFunc(out, func(a, b notify.PushSub) int { return a.Created.Compare(b.Created) })
	return out, nil
}

// UpsertPushSubscription stores a device. Subscribing again with the same
// endpoint (the browser renewed its keys) replaces the keys in place.
func (s *PB) UpsertPushSubscription(_ context.Context, sub notify.PushSub) error {
	ua := strings.TrimSpace(sub.UserAgent)
	if len(ua) > 300 {
		ua = ua[:300]
	}
	r, err := s.App.FindFirstRecordByData("push_subscriptions", "endpoint", sub.Endpoint)
	if err != nil {
		all, cerr := s.App.FindAllRecords("push_subscriptions")
		if cerr != nil {
			return cerr
		}
		if len(all) >= maxPushSubscriptions {
			return ErrTooManyDevices
		}
		col, cerr := s.App.FindCollectionByNameOrId("push_subscriptions")
		if cerr != nil {
			return cerr
		}
		r = core.NewRecord(col)
		r.Set("endpoint", sub.Endpoint)
	}
	r.Set("p256dh", sub.P256dh)
	r.Set("auth", sub.Auth)
	r.Set("user_agent", ua)
	r.Set("last_error", "")
	return s.App.Save(r)
}

// PushResult records a delivery outcome: errText empty marks a success.
// An unknown endpoint (removed meanwhile) is ignored.
func (s *PB) PushResult(_ context.Context, endpoint string, at time.Time, errText string) error {
	r, err := s.App.FindFirstRecordByData("push_subscriptions", "endpoint", endpoint)
	if err != nil {
		return nil
	}
	if errText == "" {
		var dt types.DateTime
		if err := dt.Scan(at.UTC()); err != nil {
			return err
		}
		r.Set("last_ok", dt)
		r.Set("last_error", "")
	} else {
		if len(errText) > 300 {
			errText = errText[:300]
		}
		r.Set("last_error", errText)
	}
	return s.App.Save(r)
}

// DeletePushSubscription removes a device by endpoint; unknown ones are not an error.
func (s *PB) DeletePushSubscription(_ context.Context, endpoint string) error {
	r, err := s.App.FindFirstRecordByData("push_subscriptions", "endpoint", endpoint)
	if err != nil {
		return nil
	}
	return s.App.Delete(r)
}

// DeletePushSubscriptionID removes a device by its row id.
func (s *PB) DeletePushSubscriptionID(_ context.Context, id string) error {
	r, err := s.App.FindRecordById("push_subscriptions", id)
	if err != nil {
		return nil
	}
	return s.App.Delete(r)
}
