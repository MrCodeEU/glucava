package demo

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"

	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/store"
)

// seedPush adds one made-up push device, so the Settings card shows a device
// list and the test button works. Demo mode sends through notify.DemoPushClient,
// which only logs; the endpoint is never contacted.
func seedPush(ctx context.Context, st *store.PB) error {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		return err
	}
	return st.UpsertPushSubscription(ctx, notify.PushSub{
		Endpoint:  "https://push.example.test/demo-device",
		P256dh:    base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()),
		Auth:      base64.RawURLEncoding.EncodeToString(auth),
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36",
	})
}
