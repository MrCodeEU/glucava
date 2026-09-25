package store

import (
	"context"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/MrCodeEU/glucava/internal/jobs"
)

// Config is the user-editable settings row, as the web UI shows it.
// Secrets (passwords, tokens) are never part of it; they live in the vault.
type Config struct {
	Unit           string // "mg/dL" or "mmol/L"
	RangeLow       float64
	RangeHigh      float64
	PreMin         int
	PostMin        int
	PollMin        int
	DexcomRegion   string // "us", "ous" or "jp"
	DexcomUsername string
	NtfyURL        string
	WebhookURL     string
	EmailTo        string // recipient for the email notify channel; empty disables it
	SMTPHost       string // SMTP server for email; empty disables the channel
	SMTPPort       int
	SMTPUsername   string
	SMTPTLS        bool   // enforce TLS instead of opportunistic StartTLS
	SMTPSender     string // From address
	SMTPSenderName string
	RetentionDays  int // samples and events older than this are deleted; 0 keeps them

	PublicURL     string // address of this web UI, for links in notifications; empty means no links
	MailAlerts    bool   // email failure alerts
	MailActivity  bool   // email a summary after each processed activity
	MailWeekly    bool   // email a weekly summary
	MailHealth    bool   // email a monthly health report
	ChartImage    bool   // attach a glucose chart photo to the Strava activity
	GapAlertHours int    // alert when no glucose reading arrived for this long; 0 turns it off
}

func (s *PB) settingsRecord() (*core.Record, error) {
	recs, err := s.App.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, errors.New("store: settings row missing")
	}
	return recs[0], nil
}

// LoadConfig returns the settings row.
func (s *PB) LoadConfig() (Config, error) {
	r, err := s.settingsRecord()
	if err != nil {
		return Config{}, err
	}
	return Config{
		Unit: r.GetString("unit"), RangeLow: r.GetFloat("range_low"), RangeHigh: r.GetFloat("range_high"),
		PreMin: r.GetInt("pre_minutes"), PostMin: r.GetInt("post_minutes"), PollMin: r.GetInt("poll_interval_minutes"),
		DexcomRegion: r.GetString("dexcom_region"), DexcomUsername: r.GetString("dexcom_username"),
		NtfyURL: r.GetString("ntfy_url"), WebhookURL: r.GetString("webhook_url"), EmailTo: r.GetString("email_to"),
		SMTPHost: r.GetString("smtp_host"), SMTPPort: r.GetInt("smtp_port"), SMTPUsername: r.GetString("smtp_username"),
		SMTPTLS: r.GetBool("smtp_tls"), SMTPSender: r.GetString("smtp_sender_address"), SMTPSenderName: r.GetString("smtp_sender_name"),
		RetentionDays: r.GetInt("retention_days"),
		PublicURL:     r.GetString("public_url"), MailAlerts: r.GetBool("mail_alerts"),
		MailActivity: r.GetBool("mail_activity"), MailWeekly: r.GetBool("mail_weekly"),
		MailHealth: r.GetBool("mail_health"), GapAlertHours: r.GetInt("gap_alert_hours"),
		ChartImage: r.GetBool("chart_image"),
	}, nil
}

// SaveConfig writes the settings row. The caller validates the values.
func (s *PB) SaveConfig(c Config) error {
	r, err := s.settingsRecord()
	if err != nil {
		return err
	}
	r.Set("unit", c.Unit)
	r.Set("range_low", c.RangeLow)
	r.Set("range_high", c.RangeHigh)
	r.Set("pre_minutes", c.PreMin)
	r.Set("post_minutes", c.PostMin)
	r.Set("poll_interval_minutes", c.PollMin)
	r.Set("dexcom_region", c.DexcomRegion)
	r.Set("dexcom_username", c.DexcomUsername)
	r.Set("ntfy_url", c.NtfyURL)
	r.Set("webhook_url", c.WebhookURL)
	r.Set("email_to", c.EmailTo)
	r.Set("smtp_host", c.SMTPHost)
	r.Set("smtp_port", c.SMTPPort)
	r.Set("smtp_username", c.SMTPUsername)
	r.Set("smtp_tls", c.SMTPTLS)
	r.Set("smtp_sender_address", c.SMTPSender)
	r.Set("smtp_sender_name", c.SMTPSenderName)
	r.Set("retention_days", c.RetentionDays)
	r.Set("public_url", c.PublicURL)
	r.Set("mail_alerts", c.MailAlerts)
	r.Set("mail_activity", c.MailActivity)
	r.Set("mail_weekly", c.MailWeekly)
	r.Set("mail_health", c.MailHealth)
	r.Set("gap_alert_hours", c.GapAlertHours)
	r.Set("chart_image", c.ChartImage)
	return s.App.Save(r)
}

// ListActivities returns activities newest first by start time.
func (s *PB) ListActivities(_ context.Context, limit int) ([]jobs.Activity, error) {
	recs, err := s.App.FindRecordsByFilter("activities", "", "-start_time", limit, 0)
	if err != nil {
		return nil, err
	}
	out := make([]jobs.Activity, 0, len(recs))
	for _, r := range recs {
		out = append(out, activityFromRecord(r))
	}
	return out, nil
}

// EventRow is an event as the log page shows it.
type EventRow struct {
	ID       string
	Type     string
	Severity string
	Message  string
	StravaID string
	Repaired bool
	Notified bool
	Created  time.Time
}

// CountRecentErrors counts error-severity events created at or after since.
// It backs the nav badge, so newer problems are not missed just because
// nobody opened the Notifications page.
func (s *PB) CountRecentErrors(_ context.Context, since time.Time) (int, error) {
	recs, err := s.App.FindRecordsByFilter("events", "severity = 'error' && created >= {:t}", "", 0, 0, dbx.Params{"t": pbTime(since)})
	if err != nil {
		return 0, err
	}
	return len(recs), nil
}

// ListEvents returns events newest first.
func (s *PB) ListEvents(_ context.Context, limit int) ([]EventRow, error) {
	recs, err := s.App.FindRecordsByFilter("events", "", "-created", limit, 0)
	if err != nil {
		return nil, err
	}
	out := make([]EventRow, len(recs))
	for i, r := range recs {
		out[i] = EventRow{
			ID: r.Id, Type: r.GetString("type"), Severity: r.GetString("severity"), Message: r.GetString("message"),
			StravaID: r.GetString("strava_id"), Repaired: r.GetBool("repaired"), Notified: r.GetBool("notified"),
			Created: r.GetDateTime("created").Time(),
		}
	}
	return out, nil
}

func (s *PB) lastSent(field string) time.Time {
	r, err := s.settingsRecord()
	if err != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, r.GetString(field))
	return t
}

func (s *PB) setLastSent(field string, t time.Time) error {
	r, err := s.settingsRecord()
	if err != nil {
		return err
	}
	r.Set(field, t.UTC().Format(time.RFC3339))
	return s.App.Save(r)
}

// WeeklyLast returns when the weekly summary was last sent; zero if never.
func (s *PB) WeeklyLast() time.Time { return s.lastSent("mail_weekly_last") }

// SetWeeklyLast records that the weekly summary went out at t.
func (s *PB) SetWeeklyLast(t time.Time) error { return s.setLastSent("mail_weekly_last", t) }

// HealthLast returns when the monthly health report was last sent; zero if never.
func (s *PB) HealthLast() time.Time { return s.lastSent("mail_health_last") }

// SetHealthLast records that the health report went out at t.
func (s *PB) SetHealthLast(t time.Time) error { return s.setLastSent("mail_health_last", t) }

// LatestSampleTime returns the time of the newest stored reading, from any source.
func (s *PB) LatestSampleTime(_ context.Context) (time.Time, bool) {
	recs, err := s.App.FindRecordsByFilter("glucose_samples", "", "-ts", 1, 0)
	if err != nil || len(recs) == 0 {
		return time.Time{}, false
	}
	return recs[0].GetDateTime("ts").Time(), true
}
