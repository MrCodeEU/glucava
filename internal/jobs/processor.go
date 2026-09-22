package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// Processor runs the pipeline once for one activity.
type Processor struct {
	Store      Store
	Source     glucose.Source
	SourceName string // key for stored samples, e.g. "dexcom"
	Writer     Writer
}

// Process annotates a and saves it as done. It does not save failure state;
// Queue does that after the retries are used up.
func (p *Processor) Process(ctx context.Context, a *Activity) error {
	set, err := p.Store.Settings(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	from, to := a.Start.Add(-set.Pre), a.End().Add(set.Post)

	samples, err := p.samples(ctx, from, to)
	if err != nil {
		return err
	}
	sum, ok := stats.Summarize(samples, set.Range)
	if !ok {
		return ErrNoData
	}

	if prev, perr := p.Store.Activity(ctx, a.StravaID); perr == nil && prev != nil && a.Original == nil {
		a.Original = prev.Original
	}

	block := render.Block(sum, samples, render.Options{Unit: set.Unit})
	var backupErr error
	err = p.Writer.UpdateDescription(ctx, a.StravaID, func(existing string) string {
		// Persist the backup before anything is written. If that fails, leave
		// the description untouched.
		if a.Original == nil {
			orig := render.Strip(existing)
			a.Original = &orig
			if backupErr = p.Store.SaveActivity(ctx, a); backupErr != nil {
				a.Original = nil
				return existing
			}
		}
		return render.Merge(existing, block)
	})
	if backupErr != nil {
		return fmt.Errorf("save original description: %w", backupErr)
	}
	if err != nil {
		return err
	}

	a.Status = StatusDone
	a.Error = ""
	a.Summary = &sum
	return p.Store.SaveActivity(ctx, a)
}

// samples fetches readings, stores them, and falls back to stored readings when
// the source no longer serves the window.
func (p *Processor) samples(ctx context.Context, from, to time.Time) ([]stats.Sample, error) {
	got, err := p.Source.Samples(ctx, from, to)
	switch {
	case err == nil:
		if len(got) > 0 {
			if serr := p.Store.SaveSamples(ctx, p.SourceName, got); serr != nil {
				return nil, fmt.Errorf("store samples: %w", serr)
			}
		}
		return got, nil
	case errors.Is(err, glucose.ErrTooOld):
		// Any source: a backfilled import (e.g. from another CGM app) is just
		// as usable here as the live source's own history.
		stored, lerr := p.Store.LoadSamplesAny(ctx, from, to)
		if lerr != nil {
			return nil, lerr
		}
		if len(stored) == 0 {
			return nil, err
		}
		return stored, nil
	default:
		return nil, err
	}
}

// Restore puts the description that was stored before the first edit back on Strava.
func (p *Processor) Restore(ctx context.Context, a *Activity) error {
	prev, err := p.Store.Activity(ctx, a.StravaID)
	if err != nil {
		return err
	}
	if prev == nil || prev.Original == nil {
		return ErrNoOriginal
	}
	orig := *prev.Original
	err = p.Writer.UpdateDescription(ctx, a.StravaID, func(string) string { return orig })
	if err != nil {
		return err
	}
	prev.Status, prev.Error, prev.Summary = StatusSkipped, "original description restored", nil
	return p.Store.SaveActivity(ctx, prev)
}
