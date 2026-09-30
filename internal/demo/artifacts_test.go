package demo

import (
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
)

// The demo trace must contain exactly the two artifacts it advertises: an
// overnight compression low and a sudden dip.
func TestArtifactDaysHoldOneCompressionLowAndOneDip(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	got := analytics.DetectArtifacts(artifactDays(now, nil), analytics.Thresholds{Low: 70, High: 180, VeryLow: 54, VeryHigh: 250},
		analytics.ArtifactOptions{Loc: time.UTC})
	kinds := map[analytics.ArtifactKind]int{}
	for _, a := range got {
		kinds[a.Kind]++
	}
	if kinds[analytics.ArtifactCompression] != 1 || kinds[analytics.ArtifactDip] != 1 {
		t.Errorf("artifacts = %+v", got)
	}
}
