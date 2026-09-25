package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/MrCodeEU/glucava/internal/chartimg"
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

	merge func(existing, block string) string // test seam; nil means render.Merge
}

func (p *Processor) mergeFunc() func(existing, block string) string {
	if p.merge != nil {
		return p.merge
	}
	return render.Merge
}

// Process annotates a and saves it as done. It does not save failure state;
// Queue does that after the retries are used up.
func (p *Processor) Process(ctx context.Context, a *Activity) error {
	set, err := p.Store.Settings(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	from, to := a.Start.Add(-set.Pre), a.End().Add(set.Post)
	// The chart may open earlier than the statistics do. Fetch the wider window,
	// and compute everything else on the narrower one.
	fetchFrom := from
	if set.ChartImage && a.Start.Add(-set.ChartPre).Before(fetchFrom) {
		fetchFrom = a.Start.Add(-set.ChartPre)
	}

	chartSamples, err := p.samples(ctx, fetchFrom, to)
	if err != nil {
		return err
	}
	samples := within(chartSamples, from, to)
	sum, ok := stats.Summarize(samples, set.Range)
	if !ok {
		return ErrNoData
	}

	if prev, perr := p.Store.Activity(ctx, a.StravaID); perr == nil && prev != nil {
		if a.Original == nil {
			a.Original = prev.Original
		}
		a.ChartUploaded = !a.RetryChart && (a.ChartUploaded || prev.ChartUploaded)
		if len(a.HeartRate) == 0 {
			a.HeartRate = prev.HeartRate
		}
	}

	block := render.Block(sum, samples, render.Options{Unit: set.Unit})
	var backupErr error
	unsafe := false
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
		merged := p.mergeFunc()(existing, block)
		// Last line of defence: the user's own text must survive byte for
		// byte. If it would not, leave the description as it is.
		if !render.PreservesText(existing, merged) {
			unsafe = true
			return existing
		}
		return merged
	})
	if unsafe {
		return ErrUnsafeMerge
	}
	if backupErr != nil {
		return fmt.Errorf("save original description: %w", backupErr)
	}
	if err != nil {
		return err
	}

	a.Summary = &sum
	if set.HRRead && len(a.HeartRate) == 0 {
		p.fetchHeartRate(ctx, a)
	}
	switch {
	case !set.ChartImage:
	case a.ChartUploaded:
		log.Printf("jobs: activity %s: chart photo skipped, one was attached before", a.StravaID)
	default:
		log.Printf("jobs: activity %s: attaching chart photo", a.StravaID)
		if err := p.uploadChart(ctx, a, set, chartSamples); err != nil {
			return err
		}
		log.Printf("jobs: activity %s: chart photo attached and confirmed on the edit page", a.StravaID)
	}

	a.Status = StatusDone
	a.Error = ""
	return p.Store.SaveActivity(ctx, a)
}

// uploadChart attaches the glucose chart as a photo, once per activity. The
// description is already written at this point, so a failure here is retried
// without touching the text again (the merge is idempotent).
func (p *Processor) uploadChart(ctx context.Context, a *Activity, set Settings, samples []stats.Sample) error {
	pw, ok := p.Writer.(PhotoWriter)
	if !ok {
		return nil
	}
	png, err := chartimg.Photo(chartimg.PhotoData{Samples: samples, HR: a.HeartRate, Range: set.Range, Summary: a.Summary, Start: a.Start, End: a.End(), Unit: set.Unit, Loc: time.Local, Style: set.ChartStyle})
	if err != nil {
		return fmt.Errorf("draw chart: %w", err)
	}
	if err := pw.UploadPhoto(ctx, a.StravaID, "glucose.png", png); err != nil {
		return fmt.Errorf("upload chart: %w", err)
	}
	a.ChartUploaded = true
	// Persist right away: if the final save fails, a retry must not attach it again.
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

// fetchHeartRate reads the activity's heart rate for the chart. It never fails
// the run: the chart is still worth attaching without it.
func (p *Processor) fetchHeartRate(ctx context.Context, a *Activity) {
	src, ok := p.Writer.(HRSource)
	if !ok {
		return
	}
	pts, err := src.HeartRate(ctx, a.StravaID, a.Start)
	if err != nil {
		log.Printf("jobs: activity %s: no heart rate for the chart: %v", a.StravaID, err)
		return
	}
	log.Printf("jobs: activity %s: %d heart rate points for the chart", a.StravaID, len(pts))
	a.HeartRate = pts
}

// within returns the samples inside [from, to].
func within(in []stats.Sample, from, to time.Time) []stats.Sample {
	out := make([]stats.Sample, 0, len(in))
	for _, s := range in {
		if !s.Time.Before(from) && !s.Time.After(to) {
			out = append(out, s)
		}
	}
	return out
}
