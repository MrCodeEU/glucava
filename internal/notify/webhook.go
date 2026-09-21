package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

// Webhook posts each message as JSON. With Secret set, the body is signed and the
// hex HMAC-SHA256 goes in the X-Glucava-Signature header.
type Webhook struct {
	URL    string
	Secret string
	Client *http.Client
}

// Name implements Channel.
func (w *Webhook) Name() string { return "webhook" }

// Send implements Channel.
func (w *Webhook) Send(ctx context.Context, m Message) error {
	t := m.Time
	if t.IsZero() {
		t = time.Now()
	}
	body, err := json.Marshal(map[string]any{
		"type":      m.Type,
		"severity":  m.Severity,
		"title":     m.Title,
		"message":   m.Body,
		"strava_id": m.StravaID,
		"repaired":  m.Repaired,
		"time":      t.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.Secret != "" {
		mac := hmac.New(sha256.New, []byte(w.Secret))
		mac.Write(body)
		req.Header.Set("X-Glucava-Signature", hex.EncodeToString(mac.Sum(nil)))
	}
	return do(w.Client, req, "webhook")
}
