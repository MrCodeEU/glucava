package store

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Counts reports how many rows an operation removed.
type Counts struct {
	Samples, Activities, Events int64
}

// PruneOlderThan deletes glucose samples and events created before cutoff.
// Activities stay: they are the log of what Glucava wrote to Strava.
func (s *PB) PruneOlderThan(_ context.Context, cutoff time.Time) (Counts, error) {
	var c Counts
	err := s.App.RunInTransaction(func(tx core.App) error {
		res, err := tx.DB().NewQuery("DELETE FROM glucose_samples WHERE ts < {:t}").Bind(map[string]any{"t": pbTime(cutoff)}).Execute()
		if err != nil {
			return err
		}
		c.Samples, _ = res.RowsAffected()
		res, err = tx.DB().NewQuery("DELETE FROM events WHERE created < {:t}").Bind(map[string]any{"t": pbTime(cutoff)}).Execute()
		if err != nil {
			return err
		}
		c.Events, _ = res.RowsAffected()
		return nil
	})
	if err == nil && (c.Samples > 0 || c.Events > 0) {
		s.changed()
	}
	return c, err
}

// Prune applies the retention setting. Zero days keeps everything.
func (s *PB) Prune(ctx context.Context, now time.Time) (Counts, error) {
	cfg, err := s.LoadConfig()
	if err != nil || cfg.RetentionDays <= 0 {
		return Counts{}, err
	}
	return s.PruneOlderThan(ctx, now.AddDate(0, 0, -cfg.RetentionDays))
}

// PurgeAll deletes every glucose sample, activity and event. Settings, secrets,
// API tokens and the account stay.
func (s *PB) PurgeAll(_ context.Context) (Counts, error) {
	var c Counts
	err := s.App.RunInTransaction(func(tx core.App) error {
		for table, n := range map[string]*int64{"glucose_samples": &c.Samples, "activities": &c.Activities, "events": &c.Events} {
			res, err := tx.DB().NewQuery("DELETE FROM " + table).Execute() // fixed table names
			if err != nil {
				return err
			}
			*n, _ = res.RowsAffected()
		}
		return nil
	})
	if err == nil {
		s.changed()
	}
	return c, err
}

// ExportSamples writes all glucose samples as CSV: time (UTC, RFC 3339), mg/dL, source.
func (s *PB) ExportSamples(w io.Writer) error {
	rows, err := s.App.DB().NewQuery("SELECT ts, value, source FROM glucose_samples ORDER BY ts").Rows()
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time_utc", "mg_dl", "source"})
	for rows.Next() {
		var ts, source string
		var v float64
		if err := rows.Scan(&ts, &v, &source); err != nil {
			return err
		}
		_ = cw.Write([]string{isoTime(ts), strconv.FormatFloat(v, 'f', -1, 64), safeCell(source)})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	return rows.Err()
}

// ExportActivities writes all activities with their glucose summary as CSV.
func (s *PB) ExportActivities(w io.Writer) error {
	recs, err := s.App.FindAllRecords("activities")
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"strava_id", "name", "sport", "start_utc", "duration_sec", "status", "tir_pct", "min", "max", "avg", "processed_utc"})
	for _, r := range recs {
		a := activityFromRecord(r)
		row := []string{a.StravaID, safeCell(a.Name), safeCell(a.Sport), a.Start.UTC().Format(time.RFC3339),
			strconv.Itoa(int(a.Duration.Seconds())), a.Status, "", "", "", "", isoTime(r.GetString("processed_at"))}
		if a.Summary != nil {
			row[6] = strconv.FormatFloat(a.Summary.TIR, 'f', 1, 64)
			row[7], row[8], row[9] = fmt.Sprint(a.Summary.Min), fmt.Sprint(a.Summary.Max), strconv.FormatFloat(a.Summary.Avg, 'f', 1, 64)
		}
		_ = cw.Write(row)
	}
	cw.Flush()
	return cw.Error()
}

// isoTime converts PocketBase's stored time format to RFC 3339. Unparseable or empty input is returned as is.
func isoTime(s string) string {
	if t, err := time.Parse("2006-01-02 15:04:05.000Z", s); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return s
}

// safeCell stops spreadsheet programs from running text that starts like a formula.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
