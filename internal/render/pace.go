package render

import (
	"fmt"
	"time"
)

// paceSports get a min:sec-per-km pace; speedSports get a km/h speed; Swim
// gets a min:sec-per-100m pace; anything else (Workout, WeightTraining,
// Yoga, …) has no meaningful distance metric and gets "".
var paceSports = map[string]bool{"Run": true, "TrailRun": true, "Walk": true, "Hike": true}
var speedSports = map[string]bool{"Ride": true, "VirtualRide": true, "EBikeRide": true, "Handcycle": true}

// FormatPace formats a single sport-aware pace/speed string, given sport,
// distanceM (meters) and dur (moving time). A sport with no meaningful
// distance metric, or zero distance/duration (a manual or trainer entry),
// returns "". Used both by the description template ({{.Pace}}) and the
// activity page's own tiles, so the two never disagree.
func FormatPace(sport string, distanceM float64, dur time.Duration) string {
	if distanceM <= 0 || dur <= 0 {
		return ""
	}
	switch {
	case paceSports[sport]:
		return paceString(distanceM/1000, dur) + " /km"
	case speedSports[sport]:
		return fmt.Sprintf("%.1f km/h", (distanceM/1000)/dur.Hours())
	case sport == "Swim":
		return paceString(distanceM/100, dur) + " /100m"
	default:
		return ""
	}
}

// paceString returns "m:ss" for the time it took to cover one unit of
// units (e.g. one km, one 100m), where units is distance/unitDistance.
func paceString(units float64, dur time.Duration) string {
	if units <= 0 {
		return ""
	}
	secPerUnit := int(dur.Seconds() / units)
	return fmt.Sprintf("%d:%02d", secPerUnit/60, secPerUnit%60)
}
