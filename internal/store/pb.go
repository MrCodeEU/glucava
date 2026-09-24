// Package store implements the jobs.Store interface on PocketBase.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// PB is a jobs.Store backed by PocketBase collections.
type PB struct {
	App core.App
	// Changed, if set, is called after activities or events change. The web UI uses it to refresh live views.
	Changed func()
}

func (s *PB) changed() {
	if s.Changed != nil {
		s.Changed()
	}
}

var _ jobs.Store = (*PB)(nil)

// Settings reads the singleton settings row and applies defaults for empty values.
func (s *PB) Settings(context.Context) (jobs.Settings, error) {
	recs, err := s.App.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil {
		return jobs.Settings{}, err
	}
	out := jobs.Settings{Unit: render.MgDL, Range: stats.DefaultRange, Post: 30 * time.Minute, PollInterval: 10 * time.Minute}
	if len(recs) == 0 {
		return out, nil
	}
	r := recs[0]
	if u := r.GetString("unit"); u != "" {
		out.Unit = render.Unit(u)
	}
	if lo, hi := r.GetFloat("range_low"), r.GetFloat("range_high"); lo > 0 && hi > lo {
		out.Range = stats.Range{Low: lo, High: hi}
	}
	out.Pre = time.Duration(r.GetInt("pre_minutes")) * time.Minute
	out.Post = time.Duration(r.GetInt("post_minutes")) * time.Minute
	if m := r.GetInt("poll_interval_minutes"); m > 0 {
		out.PollInterval = time.Duration(m) * time.Minute
	}
	return out, nil
}

// Activity returns nil, nil for an unknown Strava id.
func (s *PB) Activity(_ context.Context, id string) (*jobs.Activity, error) {
	r, err := s.App.FindFirstRecordByData("activities", "strava_id", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a := activityFromRecord(r)
	return &a, nil
}

func activityFromRecord(r *core.Record) jobs.Activity {
	a := jobs.Activity{
		StravaID: r.GetString("strava_id"),
		Name:     r.GetString("name"),
		Sport:    r.GetString("sport_type"),
		Start:    r.GetDateTime("start_time").Time(),
		Duration: time.Duration(r.GetInt("duration_sec")) * time.Second,
		Status:   r.GetString("status"),
		Error:    r.GetString("error"),
		Attempts: r.GetInt("attempts"),
	}
	if r.GetBool("has_original") {
		o := r.GetString("original_description")
		a.Original = &o
	}
	if raw := r.GetString("summary"); raw != "" && raw != "null" {
		var sum stats.Summary
		if json.Unmarshal([]byte(raw), &sum) == nil {
			a.Summary = &sum
		}
	}
	return a
}

// SaveActivity inserts or updates the activity keyed by StravaID.
func (s *PB) SaveActivity(_ context.Context, a *jobs.Activity) error {
	r, err := s.App.FindFirstRecordByData("activities", "strava_id", a.StravaID)
	if errors.Is(err, sql.ErrNoRows) {
		col, cerr := s.App.FindCollectionByNameOrId("activities")
		if cerr != nil {
			return cerr
		}
		r = core.NewRecord(col)
		r.Set("strava_id", a.StravaID)
	} else if err != nil {
		return err
	}
	r.Set("name", a.Name)
	r.Set("sport_type", a.Sport)
	r.Set("start_time", a.Start)
	r.Set("duration_sec", int(a.Duration.Seconds()))
	r.Set("status", a.Status)
	r.Set("error", a.Error)
	r.Set("attempts", a.Attempts)
	if a.Summary != nil {
		r.Set("summary", a.Summary)
	} else {
		r.Set("summary", nil)
	}
	if a.Original != nil { // never clear a stored backup
		r.Set("has_original", true)
		r.Set("original_description", *a.Original)
	}
	if a.Status == jobs.StatusDone {
		r.Set("processed_at", time.Now().UTC())
	}
	if err := s.App.Save(r); err != nil {
		return err
	}
	s.changed()
	return nil
}

// SaveSamples stores readings, skipping ones already present for the same source and time.
func (s *PB) SaveSamples(_ context.Context, source string, samples []stats.Sample) error {
	col, err := s.App.FindCollectionByNameOrId("glucose_samples")
	if err != nil {
		return err
	}
	return s.App.RunInTransaction(func(tx core.App) error {
		for _, smp := range samples {
			ts := types.DateTime{}
			if err := ts.Scan(smp.Time.UTC()); err != nil {
				return err
			}
			var n int
			err := tx.DB().Select("count(*)").From("glucose_samples").
				Where(dbx.HashExp{"source": source, "ts": ts}).Row(&n)
			if err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			r := core.NewRecord(col)
			r.Set("ts", ts)
			r.Set("value", smp.Value)
			r.Set("source", source)
			if err := tx.Save(r); err != nil {
				return err
			}
		}
		return nil
	})
}

// LoadSamples returns stored readings with from <= ts <= to, oldest first.
func (s *PB) LoadSamples(_ context.Context, source string, from, to time.Time) ([]stats.Sample, error) {
	recs, err := s.App.FindRecordsByFilter("glucose_samples",
		"source = {:source} && ts >= {:from} && ts <= {:to}", "ts", 0, 0,
		dbx.Params{"source": source, "from": pbTime(from), "to": pbTime(to)})
	if err != nil {
		return nil, err
	}
	out := make([]stats.Sample, len(recs))
	for i, r := range recs {
		out[i] = stats.Sample{Time: r.GetDateTime("ts").Time(), Value: r.GetFloat("value")}
	}
	return out, nil
}

// LoadSamplesAny is like LoadSamples but across every source, live or
// imported. The source column is provenance for debugging and CSV export,
// not a partition of who a reading belongs to, so annotating an activity or
// falling back for a reprocess must see all of it.
func (s *PB) LoadSamplesAny(_ context.Context, from, to time.Time) ([]stats.Sample, error) {
	recs, err := s.App.FindRecordsByFilter("glucose_samples",
		"ts >= {:from} && ts <= {:to}", "ts", 0, 0,
		dbx.Params{"from": pbTime(from), "to": pbTime(to)})
	if err != nil {
		return nil, err
	}
	out := make([]stats.Sample, len(recs))
	for i, r := range recs {
		out[i] = stats.Sample{Time: r.GetDateTime("ts").Time(), Value: r.GetFloat("value")}
	}
	return out, nil
}

// RecordEvent adds an entry to the notification outbox (notified=false).
func (s *PB) RecordEvent(_ context.Context, e jobs.Event) error {
	col, err := s.App.FindCollectionByNameOrId("events")
	if err != nil {
		return err
	}
	r := core.NewRecord(col)
	r.Set("type", e.Type)
	r.Set("severity", e.Severity)
	r.Set("message", e.Message)
	r.Set("strava_id", e.StravaID)
	r.Set("repaired", e.Repaired)
	r.Set("notified", false)
	if err := s.App.Save(r); err != nil {
		return err
	}
	s.changed()
	return nil
}

// pbTime formats t the way PocketBase stores date fields, so string comparison in filters is correct.
func pbTime(t time.Time) string {
	d, _ := types.ParseDateTime(t.UTC())
	return d.String()
}

var _ notify.Outbox = (*PB)(nil)

// Pending returns events not yet delivered, oldest first.
func (s *PB) Pending(context.Context) ([]notify.OutboxEvent, error) {
	recs, err := s.App.FindRecordsByFilter("events", "notified = false", "created", 100, 0)
	if err != nil {
		return nil, err
	}
	out := make([]notify.OutboxEvent, len(recs))
	for i, r := range recs {
		out[i] = notify.OutboxEvent{
			ID: r.Id, Type: r.GetString("type"), Severity: r.GetString("severity"),
			Message: r.GetString("message"), StravaID: r.GetString("strava_id"),
			Repaired: r.GetBool("repaired"), Created: r.GetDateTime("created").Time(),
		}
	}
	return out, nil
}

// MarkNotified flags an event as handled.
func (s *PB) MarkNotified(_ context.Context, id string) error {
	r, err := s.App.FindRecordById("events", id)
	if err != nil {
		return err
	}
	r.Set("notified", true)
	if err := s.App.Save(r); err != nil {
		return err
	}
	s.changed()
	return nil
}

// DexcomSettings returns the Dexcom region key ("us", "ous", "jp") and username.
func (s *PB) DexcomSettings() (region, username string, err error) {
	recs, err := s.App.FindRecordsByFilter("settings", "", "created", 1, 0)
	if err != nil || len(recs) == 0 {
		return "", "", err
	}
	return recs[0].GetString("dexcom_region"), recs[0].GetString("dexcom_username"), nil
}
