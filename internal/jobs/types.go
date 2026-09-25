// Package jobs runs the per-activity pipeline: fetch glucose, summarise, write to Strava.
package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/MrCodeEU/glucava/internal/chartimg"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// Activity statuses stored in the database.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDone       = "done"
	StatusFailed     = "failed"
	StatusSkipped    = "skipped"
)

// Event types, matching the select values in the events collection.
const (
	EventStravaFailed       = "strava_failed"
	EventSessionExpired     = "session_expired"
	EventGlucoseUnavailable = "glucose_unavailable"
	EventCanaryFailed       = "canary_failed"
	EventGlucoseGap         = "glucose_gap"
)

var (
	// ErrNoData means the source returned no readings for the window.
	ErrNoData = errors.New("jobs: no glucose data for the activity window")
	// ErrSessionExpired is returned by a Writer when the Strava session is invalid.
	ErrSessionExpired = errors.New("jobs: strava session expired")
	// ErrNoOriginal means no pre-edit description was stored for the activity.
	ErrNoOriginal = errors.New("jobs: no original description stored")
	// ErrUnsafeMerge means the merged description would have changed text
	// outside Glucava's own block, so nothing was written.
	ErrUnsafeMerge = errors.New("jobs: refused to write: merge would change text outside the glucava block")
	// ErrQueueFull is returned by Enqueue when the queue cannot take more work.
	ErrQueueFull = errors.New("jobs: queue full")
)

// Activity is one Strava activity to annotate.
type Activity struct {
	StravaID string
	Name     string
	Sport    string
	Start    time.Time
	Duration time.Duration

	Status   string
	Error    string
	Attempts int
	Summary  *stats.Summary

	// Original is the description before the first edit, without any Glucava
	// block. Nil means it has not been captured yet; an empty string is a valid
	// value for an activity that had no description.
	Original *string

	// ChartUploaded is set once the chart photo is on the activity. It is
	// never cleared, so a reprocess does not attach a second photo.
	ChartUploaded bool

	// RetryChart (not stored) makes this run attach the chart even though one
	// was attached before, for when the first photo never showed up on Strava.
	RetryChart bool

	// HeartRate is the activity's heart rate, thinned, once fetched. Never cleared.
	HeartRate []chartimg.HRPoint
}

// End returns the activity end time.
func (a Activity) End() time.Time { return a.Start.Add(a.Duration) }

// Settings are the runtime options the pipeline reads for each job.
type Settings struct {
	Unit  render.Unit
	Range stats.Range
	Pre   time.Duration // glucose window before the start
	Post  time.Duration // glucose window after the end

	PollInterval time.Duration // how often the poller checks Strava
	ChartImage   bool          // also attach a glucose chart photo
	ChartStyle   chartimg.Style
	ChartHR      bool // draw the activity's heart rate on the chart
}

// Event is an entry for the notification outbox.
type Event struct {
	Type     string
	Severity string // info, warning, error
	Message  string
	StravaID string
	Repaired bool
}

// Store persists pipeline state.
type Store interface {
	Settings(ctx context.Context) (Settings, error)
	// Activity returns nil, nil for an unknown id.
	Activity(ctx context.Context, stravaID string) (*Activity, error)
	SaveActivity(ctx context.Context, a *Activity) error
	SaveSamples(ctx context.Context, source string, samples []stats.Sample) error
	LoadSamples(ctx context.Context, source string, from, to time.Time) ([]stats.Sample, error)
	// LoadSamplesAny is like LoadSamples but across every source, live or
	// imported: a reading's provenance doesn't change whether it can be used
	// to annotate an activity.
	LoadSamplesAny(ctx context.Context, from, to time.Time) ([]stats.Sample, error)
	RecordEvent(ctx context.Context, e Event) error
}

// Writer updates the Strava description. It passes the current description to
// merge and saves the result. It returns ErrSessionExpired if the login is gone.
type Writer interface {
	UpdateDescription(ctx context.Context, stravaID string, merge func(existing string) string) error
}

// PhotoWriter is implemented by a Writer that can also attach a picture to the
// activity. The chart is optional, so the Processor checks for it at run time.
type PhotoWriter interface {
	UploadPhoto(ctx context.Context, stravaID, name string, png []byte) error
}

// HRSource is implemented by a Writer that can also read the activity's heart
// rate. It is optional, like PhotoWriter.
type HRSource interface {
	HeartRate(ctx context.Context, stravaID string, start time.Time) ([]chartimg.HRPoint, error)
}
