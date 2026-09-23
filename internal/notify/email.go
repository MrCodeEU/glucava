package notify

import (
	"context"
	"errors"
	"fmt"
	"net/mail"

	"github.com/pocketbase/pocketbase/tools/mailer"
)

// Email sends each message as plain-text email through the app's own SMTP
// client (Settings → Mail in the PocketBase config, applied here via the
// GLUCAVA_SMTP_* env vars — see bootstrap.ApplySMTPOverride). Glucava only
// stores the recipient; the SMTP server, sender name and address are the
// app's own mail settings, shared with the rest of PocketBase.
type Email struct {
	SendFunc func(msg *mailer.Message) error // app.NewMailClient().Send
	From     mail.Address
	To       string
}

// Name implements Channel.
func (e *Email) Name() string { return "email" }

// Send implements Channel.
func (e *Email) Send(_ context.Context, m Message) error {
	if e.To == "" {
		return errors.New("notify: email: no recipient configured")
	}
	if e.SendFunc == nil {
		return errors.New("notify: email: SMTP is not configured")
	}
	to, err := mail.ParseAddress(e.To)
	if err != nil {
		return fmt.Errorf("notify: email: invalid recipient: %w", err)
	}
	return e.SendFunc(&mailer.Message{
		From:    e.From,
		To:      []mail.Address{*to},
		Subject: m.Title,
		Text:    m.Body,
	})
}
