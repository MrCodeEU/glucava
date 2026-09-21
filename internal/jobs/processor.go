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

	block := render.Block(sum, samples, render.Options{Unit: set.Unit})
	err = p.Writer.UpdateDescription(ctx, a.StravaID, func(existing string) string {
		return render.Merge(existing, block)
	})
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
		stored, lerr := p.Store.LoadSamples(ctx, p.SourceName, from, to)
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
