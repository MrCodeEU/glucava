package strava

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseElevationThins(t *testing.T) {
	start := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	pts, err := ParseElevation([]byte(`{"altitude":[100,120,140],"time":[0,10,20]}`), start)
	if err != nil || len(pts) != 3 || pts[1].Meters != 120 || !pts[1].Time.Equal(start.Add(10*time.Second)) {
		t.Fatalf("pts = %+v, err = %v", pts, err)
	}
	var alt, tm []string
	for i := 0; i < 9394; i++ {
		alt = append(alt, "140")
		tm = append(tm, fmt.Sprint(i))
	}
	big, err := ParseElevation([]byte(`{"altitude":[`+strings.Join(alt, ",")+`],"time":[`+strings.Join(tm, ",")+`]}`), start)
	if err != nil || len(big) > maxElevPoints || len(big) < maxElevPoints/2 {
		t.Errorf("thinned to %d points, err %v", len(big), err)
	}
	if none, err := ParseElevation([]byte(`{}`), start); err != nil || len(none) != 0 {
		t.Errorf("empty stream: %v %v", none, err)
	}
	if _, err := ParseElevation([]byte(`<html>`), start); err == nil {
		t.Error("non-JSON must be an error")
	}
}
