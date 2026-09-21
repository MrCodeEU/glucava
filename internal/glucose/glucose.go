// Package glucose fetches glucose readings from a CGM data source.
package glucose

import (
	"context"
	"errors"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// Source returns glucose readings (mg/dL) with from <= Time <= to, oldest first.
type Source interface {
	Samples(ctx context.Context, from, to time.Time) ([]stats.Sample, error)
}

// ErrTooOld means the source cannot serve the requested window any more.
var ErrTooOld = errors.New("glucose: requested window is older than the source keeps")

// ErrAuth means the source rejected the credentials.
var ErrAuth = errors.New("glucose: authentication failed")
