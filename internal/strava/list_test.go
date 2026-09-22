package strava

import (
	"testing"
	"time"
)

// sampleList mirrors the real training-log response shape (checked 2026-09):
// elapsed_time/moving_time are formatted strings ("46:50"), and the numeric
// seconds live in the _raw variants. The third model has only the plain,
// numeric-string legacy shape, kept as a fallback case.
const sampleList = `{"models":[
 {"id":111,"name":"Morning Run","type":"Run","start_time":"2026-09-20T05:00:00Z","elapsed_time":"50:00","elapsed_time_raw":3000,"moving_time":"48:20","moving_time_raw":2900},
 {"id":"22'2","name":"Bad id with a quote","start_time":"2026-09-20T05:00:00Z","elapsed_time_raw":60},
 {"id":"222","name":"Local only","sport_type":"TrailRun","start_date_local_raw":1789887600,"moving_time_raw":1800},
 {"id":333,"name":"No duration","start_time":"2026-09-20T05:00:00Z"},
 {"name":"No id","start_time":"2026-09-20T05:00:00Z","elapsed_time_raw":60},
 {"id":444,"name":"No start","elapsed_time_raw":60},
 {"id":555,"name":"Only formatted, no raw","start_time":"2026-09-20T05:00:00Z","elapsed_time":"46:50","moving_time":"46:50"},
 {"id":666,"name":"Legacy numeric","start_time":"2026-09-20T05:00:00Z","elapsed_time":45}
],"total":7}`

func TestParseTrainingActivities(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	got, err := ParseTrainingActivities([]byte(sampleList), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d activities, want 3: %+v", len(got), got)
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

	// "Legacy numeric" (666): only a bare numeric elapsed_time, no _raw field.
	c := got[2]
	if c.StravaID != "666" || c.Duration != 45*time.Second {
		t.Errorf("third = %+v", c)
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
