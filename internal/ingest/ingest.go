// Package ingest continuously stores glucose readings, independent of any
// Strava activity. Without it, glucose_samples only ever gets the exact
// pre/post window of an activity that was processed while its data was still
// fresh, so an activity missed while young (the source usually keeps only
// about a day) can never be backfilled: Processor.samples falls back to
// glucose_samples, but nothing had written to it for that window. Running
// this alongside the poller keeps a rolling local history for as long as
// Settings.RetentionDays says to, so reprocessing an older activity has a
// chance of finding real data instead of failing with ErrNoData.
package ingest

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/metrics"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// Store is the subset of jobs.Store that Ingestor needs.
type Store interface {
	SaveSamples(ctx context.Context, source string, samples []stats.Sample) error
}

// Ingestor periodically fetches recent readings from Source and saves them.
type Ingestor struct {
	Source     glucose.Source
	Store      Store
	SourceName string

	Interval time.Duration // how often to fetch; default 10 minutes
	Lookback time.Duration // window to (re)fetch each time; default 20 minutes

	// MaxLookback bounds how far back a stale-gap catch-up widens the
	// window, default 23h50m (just under Dexcom Share's 24h retention, so
	// the widened request itself never trips ErrTooOld). Ignored if
	// Latest is nil.
	MaxLookback time.Duration

	// Latest returns the time of the newest stored reading, from any
	// source, e.g. store.PB.LatestSampleTime. Optional: when set, a fetch
	// whose default window wouldn't reach that reading is widened to
	// cover the gap, so reconnecting after an outage (phone off, flight
	// mode) picks up everything the source still has, not just the last
	// Lookback. When unset, every fetch uses the plain Lookback window.
	Latest func(ctx context.Context) (time.Time, bool)

	Now func() time.Time
}

func (in *Ingestor) now() time.Time {
	if in.Now != nil {
		return in.Now()
	}
	return time.Now()
}

// window returns the [from, to] to fetch for a normal tick: Lookback,
// widened to reach the newest stored reading if Latest is set and that
// reading is older than Lookback would otherwise cover, capped at
// MaxLookback.
func (in *Ingestor) window(ctx context.Context, now time.Time) time.Time {
	lookback := in.Lookback
	if lookback <= 0 {
		lookback = 20 * time.Minute
	}
	from := now.Add(-lookback)
	if in.Latest == nil {
		return from
	}
	last, ok := in.Latest(ctx)
	if !ok || last.After(from) {
		return from
	}
	maxLookback := in.MaxLookback
	if maxLookback <= 0 {
		maxLookback = 23*time.Hour + 50*time.Minute
	}
	if widened := now.Add(-maxLookback); last.Before(widened) {
		return widened
	}
	return last
}

// Once fetches and stores one window. The lookback is meant to overlap the
// previous run, so a slow tick or a brief source outage does not leave a
// gap; SaveSamples skips readings it already has for the same source and time.
func (in *Ingestor) Once(ctx context.Context) (int, error) {
	now := in.now()
	return in.fetchAndStore(ctx, in.window(ctx, now), now)
}

// ForceOnce fetches and stores the widest window the source will serve
// (MaxLookback, regardless of how fresh the newest stored reading already
// is), for a manual "resync now" request rather than waiting for a stale
// gap to trigger the same widening on the next scheduled tick.
func (in *Ingestor) ForceOnce(ctx context.Context) (int, error) {
	maxLookback := in.MaxLookback
	if maxLookback <= 0 {
		maxLookback = 23*time.Hour + 50*time.Minute
	}
	now := in.now()
	return in.fetchAndStore(ctx, now.Add(-maxLookback), now)
}

func (in *Ingestor) fetchAndStore(ctx context.Context, from, to time.Time) (int, error) {
	got, err := in.Source.Samples(ctx, from, to)
	if err != nil {
		metrics.IngestErrorsTotal.Inc()
		return 0, fmt.Errorf("ingest: %w", err)
	}
	metrics.IngestLastSuccessTimestamp.Set(float64(in.now().Unix()))
	if len(got) == 0 {
		return 0, nil
	}
	if err := in.Store.SaveSamples(ctx, in.SourceName, got); err != nil {
		return 0, fmt.Errorf("ingest: store samples: %w", err)
	}
	metrics.IngestReadingsStoredTotal.Add(float64(len(got)))
	return len(got), nil
}

// Run ticks until ctx is cancelled. Errors are logged, not fatal: a source
// hiccup should not stop later ticks from trying again.
func (in *Ingestor) Run(ctx context.Context) {
	interval := in.Interval
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := in.Once(ctx); err != nil {
			if ctx.Err() == nil { // don't log the expected error from shutting down mid-fetch
				log.Printf("%v", err)
			}
		} else if n > 0 {
			log.Printf("ingest: stored %d readings", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
