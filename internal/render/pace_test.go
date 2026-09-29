package render

import (
	"testing"
	"time"
)

func TestFormatPace(t *testing.T) {
	cases := []struct {
		name     string
		sport    string
		distance float64 // meters
		dur      time.Duration
		want     string
	}{
		{"run 10k in 50m", "Run", 10000, 50 * time.Minute, "5:00 /km"},
		{"trail run", "TrailRun", 5000, 30 * time.Minute, "6:00 /km"},
		{"ride 30km/h", "Ride", 15000, 30 * time.Minute, "30.0 km/h"},
		{"virtual ride", "VirtualRide", 20000, time.Hour, "20.0 km/h"},
		{"swim per 100m", "Swim", 1000, 20 * time.Minute, "2:00 /100m"},
		{"workout has no distance metric", "Workout", 5000, 30 * time.Minute, ""},
		{"weight training no distance metric", "WeightTraining", 0, 45 * time.Minute, ""},
		{"zero distance", "Run", 0, 30 * time.Minute, ""},
		{"zero duration", "Run", 5000, 0, ""},
		{"unknown sport", "Yoga", 100, time.Minute, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatPace(c.sport, c.distance, c.dur); got != c.want {
				t.Errorf("FormatPace(%q, %v, %v) = %q, want %q", c.sport, c.distance, c.dur, got, c.want)
			}
		})
	}
}
