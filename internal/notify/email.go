package notify

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"strings"

	"github.com/pocketbase/pocketbase/tools/mailer"
)

// Email sends each message as a multipart email (HTML card plus plain text) through the app's own SMTP
// client (Settings → Mail in the PocketBase config, applied here via the
// GLUCAVA_SMTP_* env vars — see bootstrap.ApplySMTPOverride). Glucava only
// stores the recipient; the SMTP server, sender name and address are the
// app's own mail settings, shared with the rest of PocketBase.
type Email struct {
	SendFunc func(msg *mailer.Message) error // app.NewMailClient().Send
	From     mail.Address
	To       string
	// Wants reports whether this kind of message should be mailed. Nil sends all.
	Wants func(msgType string) bool
}

// Name implements Channel.
func (e *Email) Name() string { return "email" }

// Send implements Channel.
func (e *Email) Send(_ context.Context, m Message) error {
	if e.Wants != nil && !e.Wants(m.Type) {
		return nil
	}
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
		Text:    emailText(m),
		HTML:    emailHTML(m),
	})
}

// emailText is the plain-text part: the message plus the details the HTML part shows.
func emailText(m Message) string {
	var b strings.Builder
	b.WriteString(m.Body)
	if len(m.Facts) > 0 {
		b.WriteString("\n")
		for _, f := range m.Facts {
			fmt.Fprintf(&b, "\n%s: %s", f.Label, f.Value)
		}
	}
	if m.Link != "" {
		fmt.Fprintf(&b, "\n\n%s: %s", m.LinkLabel, m.Link)
	}
	if m.StravaID != "" {
		fmt.Fprintf(&b, "\n\nStrava: https://www.strava.com/activities/%s", m.StravaID)
	}
	if m.Repaired {
		b.WriteString("\nThe selector was repaired automatically; nothing to do.")
	}
	if !m.Time.IsZero() {
		fmt.Fprintf(&b, "\n\n%s", m.Time.Format("2006-01-02 15:04 MST"))
	}
	return b.String()
}

// severityColors are the accent and the tint for a severity. The HTML uses
// inline styles only and no remote content, which mail clients keep.
func severityColors(sev string) (accent, tint string) {
	switch sev {
	case "error":
		return "#c62828", "#fdecea"
	case "warning":
		return "#b26a00", "#fff4e0"
	default:
		return "#1565c0", "#e8f1fb"
	}
}

// emailHTML renders the message as a small card. Every value is escaped.
func emailHTML(m Message) string {
	accent, tint := severityColors(m.Severity)
	esc := html.EscapeString
	sev := m.Severity
	if sev == "" {
		sev = "info"
	}

	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="margin:0;padding:24px;background:#f4f5f7;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1f2933">`)
	fmt.Fprintf(&b, `<div style="max-width:520px;margin:0 auto;background:#ffffff;border-radius:8px;border-top:4px solid %s;overflow:hidden">`, accent)
	fmt.Fprintf(&b, `<div style="padding:20px 24px 8px"><span style="display:inline-block;padding:2px 10px;border-radius:10px;background:%s;color:%s;font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.04em">%s</span>`, tint, accent, esc(sev))
	fmt.Fprintf(&b, `<h1 style="margin:12px 0 0;font-size:20px;line-height:1.3">%s</h1></div>`, esc(m.Title))
	fmt.Fprintf(&b, `<div style="padding:8px 24px 16px;font-size:15px;line-height:1.5;white-space:pre-wrap">%s</div>`, esc(m.Body))
	if len(m.Facts) > 0 {
		b.WriteString(`<table role="presentation" style="margin:0 24px 16px;width:calc(100% - 48px);border-collapse:collapse;font-size:14px">`)
		for _, f := range m.Facts {
			fmt.Fprintf(&b, `<tr><td style="padding:6px 0;border-top:1px solid #eceff1;color:#6b7280">%s</td><td style="padding:6px 0;border-top:1px solid #eceff1;text-align:right;font-weight:600">%s</td></tr>`, esc(f.Label), esc(f.Value))
		}
		b.WriteString(`</table>`)
	}
	if m.Repaired {
		b.WriteString(`<div style="margin:0 24px 16px;padding:10px 12px;background:#e8f5e9;border-radius:6px;font-size:14px">The selector was repaired automatically; nothing to do.</div>`)
	}
	var buttons []string
	if m.Link != "" {
		buttons = append(buttons, fmt.Sprintf(`<a href="%s" style="display:inline-block;margin:0 8px 8px 0;padding:8px 14px;background:%s;color:#ffffff;text-decoration:none;border-radius:6px;font-size:14px">%s</a>`, esc(m.Link), accent, esc(m.LinkLabel)))
	}
	if m.StravaID != "" {
		id := esc(m.StravaID)
		style := "background:#ffffff;color:" + accent + ";border:1px solid " + accent
		if m.Link == "" {
			style = "background:" + accent + ";color:#ffffff"
		}
		buttons = append(buttons, fmt.Sprintf(`<a href="https://www.strava.com/activities/%s" style="display:inline-block;margin:0 8px 8px 0;padding:8px 14px;%s;text-decoration:none;border-radius:6px;font-size:14px">Open activity %s on Strava</a>`, id, style, id))
	}
	if len(buttons) > 0 {
		fmt.Fprintf(&b, `<div style="padding:0 24px 12px">%s</div>`, strings.Join(buttons, ""))
	}
	foot := "glucava"
	if !m.Time.IsZero() {
		foot += " · " + m.Time.Format("2006-01-02 15:04 MST")
	}
	fmt.Fprintf(&b, `<div style="padding:12px 24px;background:#f9fafb;color:#6b7280;font-size:12px">%s</div>`, esc(foot))
	b.WriteString(`</div></body></html>`)
	return b.String()
}
