package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// pbStoredTime is how PocketBase persists date fields in SQLite.
const pbStoredTime = "2006-01-02 15:04:05.000Z"

// LoadSamplesFast returns the same readings as LoadSamplesAny (every source,
// oldest first, duplicates across sources kept) but scans raw rows instead of
// building a core.Record per reading, which is an order of magnitude cheaper
// for the multi-week ranges the stats pages load.
func (s *PB) LoadSamplesFast(_ context.Context, from, to time.Time) ([]stats.Sample, error) {
	rows, err := s.App.DB().NewQuery(
		"SELECT ts, value FROM glucose_samples WHERE ts >= {:from} AND ts <= {:to} ORDER BY ts").
		Bind(dbx.Params{"from": pbTime(from), "to": pbTime(to)}).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []stats.Sample
	for rows.Next() {
		var ts string
		var v float64
		if err := rows.Scan(&ts, &v); err != nil {
			return nil, err
		}
		t, ok := parseStored(ts)
		if !ok {
			continue
		}
		out = append(out, stats.Sample{Time: t, Value: v})
	}
	return out, rows.Err()
}

// ActivitiesInRangeLight is ActivitiesInRange without the heart_rate and
// elevation stream JSON (HeartRate and Elevation stay nil). Overview pages
// only need the summary, so skipping the streams avoids decoding megabytes.
func (s *PB) ActivitiesInRangeLight(_ context.Context, from, to time.Time) ([]jobs.Activity, error) {
	rows, err := s.App.DB().NewQuery(`SELECT strava_id, name, sport_type, start_time, duration_sec,
		distance_m, elevation_gain_m, status, error, attempts, chart_uploaded, buffer_done, summary
		FROM activities WHERE start_time >= {:from} AND start_time <= {:to} ORDER BY start_time DESC`).
		Bind(dbx.Params{"from": pbTime(from), "to": pbTime(to)}).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []jobs.Activity
	for rows.Next() {
		var (
			id, name, sport, start, status, errText, summary sql.NullString
			dur, attempts                                    sql.NullFloat64
			dist, gain                                       sql.NullFloat64
			chart, buf                                       sql.NullBool
		)
		if err := rows.Scan(&id, &name, &sport, &start, &dur, &dist, &gain, &status, &errText, &attempts, &chart, &buf, &summary); err != nil {
			return nil, err
		}
		a := jobs.Activity{
			StravaID: id.String, Name: name.String, Sport: sport.String,
			Duration: time.Duration(dur.Float64) * time.Second,
			Distance: dist.Float64, ElevationGain: gain.Float64,
			Status: status.String, Error: errText.String, Attempts: int(attempts.Float64),
			ChartUploaded: chart.Bool, BufferDone: buf.Bool,
		}
		a.Start, _ = time.Parse(pbStoredTime, start.String)
		if raw := summary.String; raw != "" && raw != "null" {
			var sum stats.Summary
			if json.Unmarshal([]byte(raw), &sum) == nil {
				a.Summary = &sum
			}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DataVersion is a cheap fingerprint of the stored readings and activities.
// Two equal values mean nothing a stats page shows can have changed, so
// callers can use it as a cache key or ETag.
type DataVersion struct {
	LatestSample   time.Time
	Samples        int64
	MaxSampleRowID int64
	ActivityUpdate time.Time
	Activities     int64
}

// DataVersion reads the current DataVersion with two aggregate queries.
func (s *PB) DataVersion(_ context.Context) (DataVersion, error) {
	var v DataVersion
	var latest, upd sql.NullString
	var maxRow sql.NullInt64
	if err := s.App.DB().NewQuery(
		"SELECT max(ts), count(*), max(rowid) FROM glucose_samples").Row(&latest, &v.Samples, &maxRow); err != nil {
		return v, err
	}
	v.LatestSample, _ = time.Parse(pbStoredTime, latest.String)
	v.MaxSampleRowID = maxRow.Int64
	if err := s.App.DB().NewQuery(
		"SELECT max(updated), count(*) FROM activities").Row(&upd, &v.Activities); err != nil {
		return v, err
	}
	v.ActivityUpdate, _ = time.Parse(pbStoredTime, upd.String)
	return v, nil
}

// parseStored is time.Parse(pbStoredTime, s) without the layout interpretation,
// which dominates the cost when scanning 100k rows. Falls back to time.Parse
// for anything not in the exact stored shape.
func parseStored(s string) (time.Time, bool) {
	if len(s) == len(pbStoredTime) && s[4] == '-' && s[7] == '-' && s[10] == ' ' && s[13] == ':' && s[16] == ':' && s[19] == '.' && s[23] == 'Z' {
		d := func(i, n int) (int, bool) {
			v := 0
			for _, c := range []byte(s[i : i+n]) {
				if c < '0' || c > '9' {
					return 0, false
				}
				v = v*10 + int(c-'0')
			}
			return v, true
		}
		y, ok1 := d(0, 4)
		mo, ok2 := d(5, 2)
		dd, ok3 := d(8, 2)
		h, ok4 := d(11, 2)
		mi, ok5 := d(14, 2)
		se, ok6 := d(17, 2)
		ms, ok7 := d(20, 3)
		if ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7 {
			return time.Date(y, time.Month(mo), dd, h, mi, se, ms*1e6, time.UTC), true
		}
	}
	t, err := time.Parse(pbStoredTime, s)
	return t, err == nil
}

// HRStat is the average and peak heart rate of one activity.
type HRStat struct{ Avg, Max float64 }

// ActivityHRStats returns average and maximum heart rate per Strava ID for
// activities in [from, to] that have a stored heart-rate series. SQLite
// aggregates the JSON so the series is never decoded in Go.
func (s *PB) ActivityHRStats(_ context.Context, from, to time.Time) (map[string]HRStat, error) {
	rows, err := s.App.DB().NewQuery(`SELECT a.strava_id,
		AVG(json_extract(h.value, '$.BPM')), MAX(json_extract(h.value, '$.BPM'))
		FROM activities a, json_each(a.heart_rate) h
		WHERE a.start_time >= {:from} AND a.start_time <= {:to}
		  AND json_valid(a.heart_rate) AND json_type(a.heart_rate) = 'array'
		GROUP BY a.strava_id`).
		Bind(dbx.Params{"from": pbTime(from), "to": pbTime(to)}).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]HRStat{}
	for rows.Next() {
		var id sql.NullString
		var avg, max sql.NullFloat64
		if err := rows.Scan(&id, &avg, &max); err != nil {
			return nil, err
		}
		if avg.Valid && avg.Float64 > 0 {
			out[id.String] = HRStat{Avg: avg.Float64, Max: max.Float64}
		}
	}
	return out, rows.Err()
}
