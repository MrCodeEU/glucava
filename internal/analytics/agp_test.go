package analytics

import (
	"math"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

func TestPercentile(t *testing.T) {
	v := []float64{10, 20, 30, 40, 50}
	for p, want := range map[float64]float64{0: 10, 25: 20, 50: 30, 75: 40, 100: 50, 10: 14, 95: 48} {
		if got := percentile(v, p); !near(got, want) {
			t.Errorf("p%v = %v, want %v", p, got, want)
		}
	}
	if got := percentile([]float64{7}, 95); got != 7 {
		t.Errorf("single = %v", got)
	}
}

func TestAGPBinsByLocalTime(t *testing.T) {
	// 08:07 Vienna (CEST, UTC+2) is 06:07 UTC: bin 8:00 in Vienna, 6:00 in UTC.
	s := []stats.Sample{{Time: time.Date(2026, 9, 20, 6, 7, 0, 0, time.UTC), Value: 100}}
	v := ComputeAGP(s, vie, 1)
	if b := v.Bins[8*4]; b.N != 1 || b.P50 != 100 || b.Minute != 480 || b.Sparse {
		t.Errorf("vienna bin = %+v", b)
	}
	u := ComputeAGP(s, nil, 1)
	if u.Bins[6*4].N != 1 {
		t.Errorf("utc bin = %+v", u.Bins[6*4])
	}
	if v.Days != 1 || v.N != 1 {
		t.Errorf("days/n = %d/%d", v.Days, v.N)
	}
}

func TestAGPSparseAndEmpty(t *testing.T) {
	a := ComputeAGP(nil, vie, 0)
	for _, b := range a.Bins {
		if !b.Sparse || b.N != 0 || b.P50 != 0 {
			t.Fatalf("empty bin = %+v", b)
		}
	}
	// 3 days of one reading at 12:00 with the default minimum of 5 is sparse.
	var s []stats.Sample
	for d := 0; d < 3; d++ {
		s = append(s, stats.Sample{Time: time.Date(2026, 9, 20+d, 12, 0, 0, 0, vie), Value: float64(100 + d*10)})
	}
	a = ComputeAGP(s, vie, 0)
	b := a.Bins[12*4]
	if !b.Sparse || b.N != 3 || b.P50 != 110 || a.Days != 3 {
		t.Errorf("bin = %+v days=%d", b, a.Days)
	}
	if a = ComputeAGP(s, vie, 3); a.Bins[12*4].Sparse {
		t.Error("bin with n == min should not be sparse")
	}
}

func TestAGPOrderedPercentiles(t *testing.T) {
	var s []stats.Sample
	for d := 0; d < 30; d++ {
		for i := 0; i < 288; i++ {
			s = append(s, stats.Sample{Time: base.AddDate(0, 0, d).Add(time.Duration(i) * 5 * time.Minute),
				Value: 80 + float64((d*7+i*3)%120)})
		}
	}
	a := ComputeAGP(s, vie, 0)
	for _, b := range a.Bins {
		if !(b.P5 <= b.P25 && b.P25 <= b.P50 && b.P50 <= b.P75 && b.P75 <= b.P95) {
			t.Fatalf("percentiles not ordered: %+v", b)
		}
	}
	if a.Days < 30 {
		t.Errorf("days = %d", a.Days)
	}
}

// AGP across the DST change: the 25-hour day (Oct 25 2026) has two 02:xx
// hours, both land in the 02:00 bins, and no reading is lost.
func TestAGPDSTFallBack(t *testing.T) {
	start := time.Date(2026, 10, 25, 0, 0, 0, 0, vie)
	end := time.Date(2026, 10, 26, 0, 0, 0, 0, vie)
	var s []stats.Sample
	for ts := start; ts.Before(end); ts = ts.Add(5 * time.Minute) {
		s = append(s, stats.Sample{Time: ts, Value: 100})
	}
	if len(s) != 300 {
		t.Fatalf("25h day has %d readings, want 300", len(s))
	}
	a := ComputeAGP(s, vie, 1)
	total := 0
	for _, b := range a.Bins {
		total += b.N
	}
	if total != 300 || a.Days != 1 {
		t.Errorf("total = %d days = %d", total, a.Days)
	}
	if a.Bins[2*4].N != 6 || a.Bins[3*4].N != 3 {
		t.Errorf("02:00 bin = %d (want 6: two passes of 3), 03:00 bin = %d (want 3)", a.Bins[2*4].N, a.Bins[3*4].N)
	}
}

func FuzzAGP(f *testing.F) {
	f.Add(int64(0), 100.0, 3)
	f.Add(int64(1_790_000_000), 54.0, 40)
	f.Add(int64(-5), 1e9, 1)
	f.Fuzz(func(t *testing.T, sec int64, v float64, n int) {
		if n < 0 || n > 500 || math.IsNaN(v) || math.IsInf(v, 0) {
			t.Skip()
		}
		// Keep within a range time.Unix and time zone conversion handle.
		sec %= 4_000_000_000
		var s []stats.Sample
		for i := 0; i < n; i++ {
			s = append(s, stats.Sample{Time: time.Unix(sec+int64(i)*300, 0), Value: v + float64(i%17)})
		}
		a := ComputeAGP(s, vie, 0)
		total := 0
		for i, b := range a.Bins {
			total += b.N
			if b.Minute != i*15 || b.Minute < 0 || b.Minute >= 1440 {
				t.Fatalf("bin %d minute %d", i, b.Minute)
			}
			if b.N > 0 && !(b.P5 <= b.P25 && b.P25 <= b.P50 && b.P50 <= b.P75 && b.P75 <= b.P95) {
				t.Fatalf("unordered %+v", b)
			}
		}
		if total != n {
			t.Fatalf("binned %d of %d", total, n)
		}
	})
}
