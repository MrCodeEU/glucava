package notify

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"testing"
	"time"

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

func TestEmailHTMLEscapesAndKeepsTextFallback(t *testing.T) {
	var got *mailer.Message
	e := &Email{SendFunc: func(m *mailer.Message) error { got = m; return nil }, To: "me@example.com"}
	msg := Message{
		Type: "strava_failed", Severity: "error", Title: "Update <failed>",
		Body: `boom <script>alert(1)</script> & "quote"`, StravaID: "140098", Repaired: true,
		Time: time.Date(2026, 9, 24, 18, 0, 0, 0, time.UTC),
	}
	if err := e.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.HTML, "<script>") || !strings.Contains(got.HTML, "&lt;script&gt;") {
		t.Errorf("HTML did not escape the body: %s", got.HTML)
	}
	for _, want := range []string{"Update &lt;failed&gt;", "https://www.strava.com/activities/140098", "repaired automatically", "2026-09-24 18:00"} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("HTML is missing %q", want)
		}
	}
	for _, want := range []string{msg.Body, "https://www.strava.com/activities/140098", "repaired automatically"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("text part is missing %q: %s", want, got.Text)
		}
	}
	if strings.Contains(got.HTML, "http://") || strings.Contains(got.HTML, "src=") {
		t.Error("the HTML must not load remote content")
	}
}

func TestEmailHTMLSeverityDefaultsToInfo(t *testing.T) {
	if h := emailHTML(Message{Title: "t", Body: "b"}); !strings.Contains(h, ">info<") {
		t.Errorf("empty severity should render as info: %s", h)
	}
}

func TestEmailWantsFiltersKinds(t *testing.T) {
	calls := 0
	e := &Email{
		SendFunc: func(*mailer.Message) error { calls++; return nil }, To: "me@example.com",
		Wants: func(typ string) bool { return typ == "strava_failed" },
	}
	if err := e.Send(context.Background(), Message{Type: "weekly_summary"}); err != nil || calls != 0 {
		t.Fatalf("unwanted kind sent: err=%v calls=%d", err, calls)
	}
	if err := e.Send(context.Background(), Message{Type: "strava_failed"}); err != nil || calls != 1 {
		t.Fatalf("wanted kind not sent: err=%v calls=%d", err, calls)
	}
}

func TestEmailRendersFactsAndLink(t *testing.T) {
	var got *mailer.Message
	e := &Email{SendFunc: func(m *mailer.Message) error { got = m; return nil }, To: "me@example.com"}
	err := e.Send(context.Background(), Message{
		Type: "weekly_summary", Title: "Weekly", Body: "b", Link: "https://g.example/", LinkLabel: "Open dashboard",
		Facts: []Fact{{"Activities", "3"}, {"Best <b>", "92%"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="https://g.example/"`, "Open dashboard", "Activities", "Best &lt;b&gt;", "92%"} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	for _, want := range []string{"Activities: 3", "Open dashboard: https://g.example/"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("text missing %q: %s", want, got.Text)
		}
	}
}

func TestEmailInlineChartAndIcon(t *testing.T) {
	var got *mailer.Message
	e := &Email{SendFunc: func(m *mailer.Message) error { got = m; return nil }, To: "me@example.com"}
	if err := e.Send(context.Background(), Message{Type: "session_expired", Title: "T", Body: "b", Chart: []byte("PNG"), ChartAlt: "a <chart>"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.InlineAttachments["chart.png"]; !ok {
		t.Fatal("chart not attached inline")
	}
	for _, want := range []string{`src="cid:chart.png"`, `alt="a &lt;chart&gt;"`, "\U0001F511"} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if err := e.Send(context.Background(), Message{Type: "strava_failed", Title: "T", Icon: "X"}); err != nil {
		t.Fatal(err)
	}
	if len(got.InlineAttachments) != 0 || strings.Contains(got.HTML, "cid:") || !strings.Contains(got.HTML, ">X<") {
		t.Errorf("no chart means no attachment; own icon wins: %v", got.HTML)
	}
}

func TestIconForEveryAlertType(t *testing.T) {
	for typ := range titles {
		if _, ok := icons[typ]; !ok {
			t.Errorf("no icon for %s", typ)
		}
	}
	if IconFor(Message{Type: "unknown"}) == "" {
		t.Error("fallback icon missing")
	}
}
