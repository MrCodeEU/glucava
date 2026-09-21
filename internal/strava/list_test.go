package strava

import (
	"testing"
	"time"
)

const sampleList = `{"models":[
 {"id":111,"name":"Morning Run","type":"Run","start_time":"2026-09-20T05:00:00Z","elapsed_time":3000,"moving_time":2900},
 {"id":"22'2","name":"Bad id with a quote","start_time":"2026-09-20T05:00:00Z","elapsed_time":60},
 {"id":"222","name":"Local only","sport_type":"TrailRun","start_date_local_raw":1789887600,"moving_time":1800},
 {"id":333,"name":"No duration","start_time":"2026-09-20T05:00:00Z"},
 {"name":"No id","start_time":"2026-09-20T05:00:00Z","elapsed_time":60},
 {"id":444,"name":"No start","elapsed_time":60}
],"total":5}`

func TestParseTrainingActivities(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	got, err := ParseTrainingActivities([]byte(sampleList), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d activities, want 2: %+v", len(got), got)
	}

	a := got[0]
	if a.StravaID != "111" || a.Sport != "Run" || a.Duration != 50*time.Minute ||
		!a.Start.Equal(time.Date(2026, 9, 20, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("first = %+v", a)
	}

	// 1789887600 = 2026-09-20 07:00:00 as a wall clock; Berlin is UTC+2 in September.
	b := got[1]
	if b.StravaID != "222" || b.Sport != "TrailRun" || b.Duration != 30*time.Minute ||
		!b.Start.Equal(time.Date(2026, 9, 20, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("second = %+v (start %v)", b, b.Start)
	}
}

func TestParseTrainingActivitiesErrors(t *testing.T) {
	if _, err := ParseTrainingActivities([]byte("<html>"), nil); err == nil {
		t.Error("expected error for non-JSON")
	}
	got, err := ParseTrainingActivities([]byte(`{"models":[]}`), nil)
	if err != nil || len(got) != 0 {
		t.Errorf("empty = %v, %v", got, err)
	}
}
