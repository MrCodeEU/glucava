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
	Lang           string // "en" or "de"
	DexcomRegion   string // "us", "ous" or "jp"
	DexcomUsername string
	NtfyURL        string
	WebhookURL     string
	RetentionDays  int // samples and events older than this are deleted; 0 keeps them
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
		Lang: r.GetString("lang"), DexcomRegion: r.GetString("dexcom_region"), DexcomUsername: r.GetString("dexcom_username"),
		NtfyURL: r.GetString("ntfy_url"), WebhookURL: r.GetString("webhook_url"),
		RetentionDays: r.GetInt("retention_days"),
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
	r.Set("lang", c.Lang)
	r.Set("dexcom_region", c.DexcomRegion)
	r.Set("dexcom_username", c.DexcomUsername)
	r.Set("ntfy_url", c.NtfyURL)
	r.Set("webhook_url", c.WebhookURL)
	r.Set("retention_days", c.RetentionDays)
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
