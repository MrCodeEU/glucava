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
	Unit      string // "mg/dL" or "mmol/L"
	RangeLow  float64
	RangeHigh float64
	// VeryLow and VeryHigh are the level-2 hypo/hyper thresholds (mg/dL);
	// zero means the consensus defaults 54 and 250.
	VeryLow        float64
	VeryHigh       float64
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

	PublicURL      string // address of this web UI, for links in notifications; empty means no links
	MailAlerts     bool   // email failure alerts
	MailActivity   bool   // email a summary after each processed activity
	MailWeekly     bool   // email a weekly summary
	MailHealth     bool   // email a monthly health report
	PushAlerts     bool   // push failure alerts to subscribed devices
	PushSummaries  bool   // push activity, weekly and health summaries
	ChartImage     bool   // attach a glucose chart photo to the Strava activity
	ChartTheme     string // "light" or "dark"
	ChartSize      string // "standard" or "large"
	ChartBand      bool   // shade the target range
	ChartActivity  bool   // shade the activity span
	ChartDots      bool   // mark out-of-range readings
	ChartLine      int    // curve thickness, 1 to 4
	ChartHR        bool   // draw heart rate on the chart
	ChartElevation bool   // draw the elevation profile on the chart
	// ChartPanelOrder is a comma-separated list of enabled panel names
	// ("activity", "band", "dots", "hr", "elevation"), draw order first to last; a
	// panel whose own boolean is on but is missing here still shows,
	// appended at the end (see Config.ChartStyle). Empty means the
	// default order.
	ChartPanelOrder string
	ChartPreMin     int  // minutes of glucose before the activity on the chart
	ChartAvgLine    bool // dashed line at the average glucose value
	ChartRangeLines bool // dashed lines at the target range low/high
	ChartMinMax     bool // marker dots at the curve's minimum and maximum
	ChartHideStats  bool // hide the TIR header number and below/in-range/above bar
	HRRead          bool // read heart rate from Strava for stats and charts
	GapAlertHours   int  // alert when no glucose reading arrived for this long; 0 turns it off
	PostBufferMin   int  // minutes after the glucose window closes to reprocess once more; 0 disables it

	// DescriptionTemplate is a Go text/template (see render.RenderBlock).
	// Empty means render.DefaultTemplate, today's built-in wording.
	DescriptionTemplate string

	// OverviewLayout is the Overview page's cards as JSON: an ordered list
	// of {id, enabled, options} (see OverviewCard). Empty means every card,
	// enabled, in the default order; Config.OverviewCards normalizes it.
	OverviewLayout string

	// OverviewDefaultRange is the range preset the Overview opens with: one
	// of OverviewRanges, empty meaning 30d.
	OverviewDefaultRange string

	// ArtifactMode says what the Overview does with suspected CGM artifacts
	// (ArtifactFlagged or ArtifactExclude); empty means flagged.
	ArtifactMode string

	// Language is the interface language: a locale tag from internal/i18n
	// ("de"), or "auto"/empty to follow the browser's Accept-Language.
	Language string
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
		VeryLow: r.GetFloat("very_low"), VeryHigh: r.GetFloat("very_high"),
		PreMin: r.GetInt("pre_minutes"), PostMin: r.GetInt("post_minutes"), PollMin: r.GetInt("poll_interval_minutes"),
		DexcomRegion: r.GetString("dexcom_region"), DexcomUsername: r.GetString("dexcom_username"),
		NtfyURL: r.GetString("ntfy_url"), WebhookURL: r.GetString("webhook_url"), EmailTo: r.GetString("email_to"),
		SMTPHost: r.GetString("smtp_host"), SMTPPort: r.GetInt("smtp_port"), SMTPUsername: r.GetString("smtp_username"),
		SMTPTLS: r.GetBool("smtp_tls"), SMTPSender: r.GetString("smtp_sender_address"), SMTPSenderName: r.GetString("smtp_sender_name"),
		RetentionDays: r.GetInt("retention_days"),
		PublicURL:     r.GetString("public_url"), MailAlerts: r.GetBool("mail_alerts"),
		MailActivity: r.GetBool("mail_activity"), MailWeekly: r.GetBool("mail_weekly"),
		MailHealth: r.GetBool("mail_health"), GapAlertHours: r.GetInt("gap_alert_hours"),
		ChartImage: r.GetBool("chart_image"), ChartTheme: r.GetString("chart_theme"), ChartSize: r.GetString("chart_size"),
		ChartBand: r.GetBool("chart_band"), ChartActivity: r.GetBool("chart_activity"), ChartDots: r.GetBool("chart_dots"),
		ChartLine: r.GetInt("chart_line"), ChartHR: r.GetBool("chart_hr"), ChartElevation: r.GetBool("chart_elevation"),
		ChartPanelOrder: r.GetString("chart_panel_order"),
		ChartPreMin:     r.GetInt("chart_pre_minutes"), HRRead: r.GetBool("hr_read"),
		ChartAvgLine: r.GetBool("chart_avg_line"), ChartRangeLines: r.GetBool("chart_range_lines"),
		ChartMinMax: r.GetBool("chart_min_max"), ChartHideStats: r.GetBool("chart_hide_stats"),
		PostBufferMin:        r.GetInt("post_buffer_minutes"),
		DescriptionTemplate:  r.GetString("description_template"),
		OverviewLayout:       r.GetString("overview_layout"),
		OverviewDefaultRange: r.GetString("overview_default_range"),
		ArtifactMode:         r.GetString("artifact_mode"),
		PushAlerts:           r.GetBool("push_alerts"), PushSummaries: r.GetBool("push_summaries"),
		Language: r.GetString("language"),
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
	r.Set("very_low", c.VeryLow)
	r.Set("very_high", c.VeryHigh)
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
	r.Set("chart_theme", c.ChartTheme)
	r.Set("chart_size", c.ChartSize)
	r.Set("chart_band", c.ChartBand)
	r.Set("chart_activity", c.ChartActivity)
	r.Set("chart_dots", c.ChartDots)
	r.Set("chart_line", c.ChartLine)
	r.Set("chart_panel_order", c.ChartPanelOrder)
	r.Set("chart_hr", c.ChartHR)
	r.Set("chart_elevation", c.ChartElevation)
	r.Set("chart_pre_minutes", c.ChartPreMin)
	r.Set("chart_avg_line", c.ChartAvgLine)
	r.Set("chart_range_lines", c.ChartRangeLines)
	r.Set("chart_min_max", c.ChartMinMax)
	r.Set("chart_hide_stats", c.ChartHideStats)
	r.Set("hr_read", c.HRRead)
	r.Set("post_buffer_minutes", c.PostBufferMin)
	r.Set("description_template", c.DescriptionTemplate)
	r.Set("overview_layout", c.OverviewLayout)
	r.Set("overview_default_range", c.OverviewDefaultRange)
	r.Set("artifact_mode", c.ArtifactMode)
	r.Set("push_alerts", c.PushAlerts)
	r.Set("push_summaries", c.PushSummaries)
	r.Set("language", c.Language)
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

// RecoverStuck resets activities left at "processing" back to "pending" and
// returns them. A record only stays at that status while a worker holds it in
// memory; after a restart nothing does. The poller leaves any known activity
// alone regardless of status (so it never double-processes one the queue is
// already working on), so without this an activity interrupted mid-run (e.g.
// by a host reboot) would sit invisible to both the poller and a retry,
// forever. The caller re-enqueues what comes back. Called once at startup.
func (s *PB) RecoverStuck(_ context.Context) ([]jobs.Activity, error) {
	recs, err := s.App.FindRecordsByFilter("activities", "status = {:status}", "", 0, 0,
		dbx.Params{"status": jobs.StatusProcessing})
	if err != nil {
		return nil, err
	}
	out := make([]jobs.Activity, 0, len(recs))
	for _, r := range recs {
		r.Set("status", jobs.StatusPending)
		r.Set("error", "")
		if err := s.App.Save(r); err != nil {
			return nil, err
		}
		a := activityFromRecord(r)
		a.Status = jobs.StatusPending
		out = append(out, a)
	}
	return out, nil
}

// DueForBuffer returns done activities not yet reprocessed once by the
// PostBuffer delay, started at or after since. The poller filters by end
// time + Post + PostBuffer itself, since that needs Settings.
func (s *PB) DueForBuffer(_ context.Context, since time.Time, limit int) ([]jobs.Activity, error) {
	recs, err := s.App.FindRecordsByFilter("activities",
		"status = {:status} && buffer_done = false && start_time >= {:since}",
		"-start_time", limit, 0, dbx.Params{"status": jobs.StatusDone, "since": since})
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
