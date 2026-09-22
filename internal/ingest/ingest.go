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

	Now func() time.Time
}

func (in *Ingestor) now() time.Time {
	if in.Now != nil {
		return in.Now()
	}
	return time.Now()
}

// Once fetches and stores one window. The lookback is meant to overlap the
// previous run, so a slow tick or a brief source outage does not leave a
// gap; SaveSamples skips readings it already has for the same source and time.
func (in *Ingestor) Once(ctx context.Context) (int, error) {
	lookback := in.Lookback
	if lookback <= 0 {
		lookback = 20 * time.Minute
	}
	now := in.now()
	got, err := in.Source.Samples(ctx, now.Add(-lookback), now)
	if err != nil {
		// ErrTooOld cannot happen for a recent window; anything else (session/auth
		// trouble, a down source) is expected to recur and is logged by Run.
		return 0, fmt.Errorf("ingest: %w", err)
	}
	if len(got) == 0 {
		return 0, nil
	}
	if err := in.Store.SaveSamples(ctx, in.SourceName, got); err != nil {
		return 0, fmt.Errorf("ingest: store samples: %w", err)
	}
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
