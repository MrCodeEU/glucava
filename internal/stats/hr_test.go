package stats

import (
	"testing"
	"time"
)

func TestSummarizeHRWindowAndEmpty(t *testing.T) {
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	pts := []HRSample{{at, 100}, {at.Add(time.Minute), 140}, {at.Add(2 * time.Minute), 180}, {at.Add(time.Hour), 60}}
	s, ok := SummarizeHR(pts, at, at.Add(5*time.Minute))
	if !ok || s.Count != 3 || s.Min != 100 || s.Max != 180 || s.Avg != 140 {
		t.Errorf("window = %+v %v", s, ok)
	}
	if all, _ := SummarizeHR(pts, time.Time{}, time.Time{}); all.Count != 4 || all.Min != 60 {
		t.Errorf("open window = %+v", all)
	}
	if _, ok := SummarizeHR(pts, at.Add(2*time.Hour), time.Time{}); ok {
		t.Error("no readings in the window must report not ok")
	}
	if _, ok := SummarizeHR(nil, time.Time{}, time.Time{}); ok {
		t.Error("nil must report not ok")
	}
}
