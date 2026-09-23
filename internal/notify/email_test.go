package notify

import (
	"context"
	"errors"
	"net/mail"
	"testing"

	"github.com/pocketbase/pocketbase/tools/mailer"
)

func TestEmailSendsToConfiguredRecipient(t *testing.T) {
	var got *mailer.Message
	e := &Email{
		SendFunc: func(m *mailer.Message) error { got = m; return nil },
		From:     mail.Address{Name: "glucava", Address: "glucava@example.com"},
		To:       "me@example.com",
	}
	msg := Message{Type: "strava_failed", Title: "Strava update failed", Body: "could not save"}
	if err := e.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got == nil {
		t.Fatal("SendFunc was not called")
	}
	if got.From.Address != "glucava@example.com" || len(got.To) != 1 || got.To[0].Address != "me@example.com" {
		t.Errorf("message = %+v", got)
	}
	if got.Subject != msg.Title || got.Text != msg.Body {
		t.Errorf("subject/body = %q/%q", got.Subject, got.Text)
	}
}

func TestEmailNoRecipientErrors(t *testing.T) {
	e := &Email{SendFunc: func(*mailer.Message) error { return nil }}
	if err := e.Send(context.Background(), Message{}); err == nil {
		t.Error("expected an error with no recipient configured")
	}
}

func TestEmailInvalidRecipientErrors(t *testing.T) {
	e := &Email{SendFunc: func(*mailer.Message) error { return nil }, To: "not-an-address"}
	if err := e.Send(context.Background(), Message{}); err == nil {
		t.Error("expected an error for an invalid recipient")
	}
}

func TestEmailNoSendFuncErrors(t *testing.T) {
	e := &Email{To: "me@example.com"}
	if err := e.Send(context.Background(), Message{}); err == nil {
		t.Error("expected an error with SMTP not configured")
	}
}

func TestEmailPropagatesSendError(t *testing.T) {
	e := &Email{SendFunc: func(*mailer.Message) error { return errors.New("smtp down") }, To: "me@example.com"}
	if err := e.Send(context.Background(), Message{}); err == nil {
		t.Error("expected the underlying send error to propagate")
	}
}

func TestEmailName(t *testing.T) {
	if (&Email{}).Name() != "email" {
		t.Error("Name() should be \"email\"")
	}
}
