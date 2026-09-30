package analytics

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

var insThr = Thresholds{VeryLow: 54, Low: 70, High: 180, VeryHigh: 250}

// ser5 makes readings every 5 minutes from t0, one per value.
func ser5(t0 time.Time, vals ...float64) []stats.Sample {
	out := make([]stats.Sample, len(vals))
	for i, v := range vals {
		out[i] = stats.Sample{Time: t0.Add(time.Duration(i) * 5 * time.Minute), Value: v}
	}
	return out
}

func TestInsightBasics(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	// 10:00-10:30 pre (2 readings before start at 10:30? build explicitly)
	s := ser5(t0, 100, 102, 104, 120, 110, 100, 90, 80, 70, 100, 100, 100, 100, 100, 100)
	// activity 10:30-10:50 -> readings idx 6..10 = 90,80,70,100,100? idx6=10:30
	a := ActivityInput{ID: "a", Sport: "Run", Start: t0.Add(30 * time.Minute), End: t0.Add(50 * time.Minute),
		AvgHR: 150, DistanceM: 4000}
	got := InsightsFor(s, []ActivityInput{a}, insThr, time.UTC)
	if len(got) != 1 {
		t.Fatal(len(got))
	}
	in := got[0]
	if !in.HasData || in.StartGlucose != 90 {
		t.Fatalf("%+v", in)
	}
	// during = idx 6..10 => 90,80,70,100,100 ; last is 10:50 = idx 10.
	if in.EndGlucose != 100 || in.Delta != 10 {
		t.Errorf("end/delta %v %v", in.EndGlucose, in.Delta)
	}
	if !near(in.DropRate, -10.0/20*10) { // rose 10 in 20 min
		t.Errorf("drop %v", in.DropRate)
	}
	if !near(in.Avg, 88) || in.Min != 70 || in.Max != 100 {
		t.Errorf("avg/min/max %v %v %v", in.Avg, in.Min, in.Max)
	}
	// pre 10:00-10:29: idx 0..5 = 100,102,104,120,110,100 -> mean 106
	if !in.HasPre || !near(in.PreMean, 106) {
		t.Errorf("pre %v %v", in.PreMean, in.HasPre)
	}
	// post 10:50+ to 11:50: idx 11..14 = 100 each
	if !in.HasPost || !near(in.PostMean, 100) {
		t.Errorf("post %v", in.PostMean)
	}
	if !near(in.SpeedMS, 4000.0/1200) {
		t.Errorf("speed %v", in.SpeedMS)
	}
	if in.PostLow() {
		t.Errorf("unexpected post low")
	}
}

func TestInsightNoSamplesInsideActivity(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	s := ser5(t0, 100, 100)
	a := ActivityInput{Sport: "Run", Start: t0.Add(2 * time.Hour), End: t0.Add(3 * time.Hour)}
	in := InsightsFor(s, []ActivityInput{a}, insThr, time.UTC)[0]
	if in.HasData || in.Delta != 0 || in.HasPre || in.HasPost {
		t.Errorf("%+v", in)
	}
	if got := InsightsFor(nil, []ActivityInput{a}, insThr, time.UTC); len(got) != 1 || got[0].HasData {
		t.Errorf("no samples: %+v", got)
	}
	if got := InsightsFor(s, nil, insThr, time.UTC); got != nil {
		t.Errorf("no activities: %v", got)
	}
}

func TestInsightSingleReadingHasZeroDropRate(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	a := ActivityInput{Start: t0, End: t0.Add(time.Minute)}
	in := InsightsFor(ser5(t0, 120), []ActivityInput{a}, insThr, time.UTC)[0]
	if !in.HasData || in.DropRate != 0 || in.Delta != 0 {
		t.Errorf("%+v", in)
	}
}

func TestInsightRangeEdgesInclusive(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	// Readings exactly at start and end belong to "during", not pre/post.
	s := ser5(t0, 100, 110, 120)
	a := ActivityInput{Start: t0, End: t0.Add(10 * time.Minute)}
	in := InsightsFor(s, []ActivityInput{a}, insThr, time.UTC)[0]
	if in.StartGlucose != 100 || in.EndGlucose != 120 || in.HasPre || in.HasPost {
		t.Errorf("%+v", in)
	}
}

func TestInsightPostActivityLow(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	// Activity 10:00-10:20. Then a 20 minute low starting 11:00 (within 3h).
	vals := []float64{110, 110, 110, 110, 110, // 10:00-10:20
		110, 110, 110, 110, 110, 110, 110, 110, // 10:25-11:00 (idx 5..12); make 11:00 low below
	}
	s := ser5(t0, vals...)
	s[12].Value = 65
	s = append(s, ser5(t0.Add(65*time.Minute), 60, 62, 64, 66, 110)...) // 11:05..11:25 low, then back
	a := ActivityInput{Sport: "Run", Start: t0, End: t0.Add(20 * time.Minute)}
	in := InsightsFor(s, []ActivityInput{a}, insThr, time.UTC)[0]
	if in.PostLows != 1 || in.PostLowNadir != 60 {
		t.Fatalf("%+v", in)
	}
	// A shorter window misses it.
	short := InsightsWith(s, []ActivityInput{a}, insThr, time.UTC, InsightOptions{PostLowWithin: 30 * time.Minute})[0]
	if short.PostLow() {
		t.Errorf("30 min window should miss the low")
	}
	// A low already running when the activity ends does not count.
	b := ActivityInput{Start: t0.Add(50 * time.Minute), End: t0.Add(70 * time.Minute)}
	if InsightsFor(s, []ActivityInput{b}, insThr, time.UTC)[0].PostLow() {
		t.Errorf("low running at activity end must not count")
	}
}

func TestInsightUnsortedInput(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	s := ser5(t0, 100, 110, 120, 130)
	rev := []stats.Sample{s[3], s[1], s[0], s[2]}
	a := ActivityInput{Start: t0, End: t0.Add(15 * time.Minute)}
	x := InsightsFor(s, []ActivityInput{a}, insThr, time.UTC)[0]
	y := InsightsFor(rev, []ActivityInput{a}, insThr, time.UTC)[0]
	if x != y {
		t.Errorf("order changed result: %+v vs %+v", x, y)
	}
}

// A run across the spring-forward night in Vienna (2026-03-29 02:00 -> 03:00)
// must use elapsed time, not wall-clock, for the drop rate.
func TestInsightAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Skip("no tzdata")
	}
	start := time.Date(2026, 3, 29, 1, 30, 0, 0, loc) // 00:30 UTC
	s := ser5(start, 200, 190, 180, 170, 160, 150, 140)
	a := ActivityInput{Start: start, End: start.Add(30 * time.Minute)}
	in := InsightsFor(s, []ActivityInput{a}, insThr, loc)[0]
	// 30 elapsed minutes, fell 60 mg/dL => 20 per 10 minutes.
	if !near(in.DropRate, 20) {
		t.Errorf("drop %v", in.DropRate)
	}
}

func TestBySport(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	mk := func(sport string, tir, delta, cv float64, hr float64, dist float64, dur time.Duration, has bool, postLows int) ActivityInsight {
		return ActivityInsight{
			ActivityInput: ActivityInput{Sport: sport, Start: t0, End: t0.Add(dur), AvgHR: hr, DistanceM: dist, ElevationGain: 10},
			HasData:       has, TIR: TIR5{InRange: tir, Count: 1}, Delta: delta, CV: cv, DropRate: -delta, PostLows: postLows,
		}
	}
	ins := []ActivityInsight{
		mk("Run", 100, -20, 10, 150, 5000, 30*time.Minute, true, 0),
		mk("Run", 60, -40, 20, 0, 10000, 60*time.Minute, true, 1),
		mk("Run", 0, 0, 0, 0, 0, 10*time.Minute, false, 0), // no data: excluded from glucose means
		mk("Ride", 90, 5, 8, 120, 20000, 60*time.Minute, true, 0),
	}
	got := BySport(ins)
	if len(got) != 2 || got[0].Sport != "Run" || got[1].Sport != "Ride" {
		t.Fatalf("%+v", got)
	}
	r := got[0]
	if r.Count != 3 || r.WithData != 2 || !near(r.TIR, 80) || !near(r.Delta, -30) || !near(r.CV, 15) || !near(r.PostLowShare, 50) {
		t.Errorf("run %+v", r)
	}
	if !near(r.AvgHR, 150) || !near(r.TotalDistance, 15000) || !near(r.AvgDistance, 7500) || !near(r.TotalElevation, 30) {
		t.Errorf("run totals %+v", r)
	}
	if !near(r.AvgSpeedMS, 15000.0/(90*60)) {
		t.Errorf("speed %v", r.AvgSpeedMS)
	}
	if BySport(nil) == nil || len(BySport(nil)) != 0 {
		t.Errorf("nil input should give an empty, non-nil slice")
	}
}

func TestScatterAndBestWorst(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	mk := func(id string, tir float64, has bool, day int) ActivityInsight {
		return ActivityInsight{ActivityInput: ActivityInput{ID: id, Start: t0.AddDate(0, 0, day)},
			HasData: has, TIR: TIR5{InRange: tir}, StartGlucose: 100, Delta: -10}
	}
	ins := []ActivityInsight{mk("a", 50, true, 0), mk("b", 100, true, 1), mk("c", 0, false, 2), mk("d", 80, true, 3), mk("e", 100, true, 4)}
	if sc := Scatter(ins); len(sc) != 4 || sc[0].ID != "a" || sc[0].X != 100 || sc[0].Y != -10 {
		t.Errorf("scatter %+v", sc)
	}
	best, worst := BestWorst(ins, 2)
	if len(best) != 2 || best[0].ID != "b" || best[1].ID != "e" { // tie broken by earlier start
		t.Errorf("best %v", ids(best))
	}
	if len(worst) != 2 || worst[0].ID != "a" || worst[1].ID != "d" {
		t.Errorf("worst %v", ids(worst))
	}
	// Fewer candidates than 2n: no overlap.
	best, worst = BestWorst(ins, 3)
	if len(best) != 3 || len(worst) != 1 || worst[0].ID != "a" {
		t.Errorf("overlap case best %v worst %v", ids(best), ids(worst))
	}
	if b, w := BestWorst(nil, 3); b != nil || w != nil {
		t.Errorf("empty")
	}
	if b, w := BestWorst(ins, 0); b != nil || w != nil {
		t.Errorf("n=0")
	}
}

func ids(in []ActivityInsight) []string {
	var out []string
	for _, i := range in {
		out = append(out, i.ID)
	}
	return out
}

func TestKPIsAndDelta(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var cur, prev []stats.Sample
	for i := 0; i < 288*2; i++ { // two full days, all in range at 100
		cur = append(cur, stats.Sample{Time: t0.Add(time.Duration(i) * 5 * time.Minute), Value: 100})
	}
	p0 := t0.Add(-48 * time.Hour)
	for i := 0; i < 288*2; i++ { // two days before: alternate 100 and 200
		v := 100.0
		if i%2 == 1 {
			v = 200
		}
		prev = append(prev, stats.Sample{Time: p0.Add(time.Duration(i) * 5 * time.Minute), Value: v})
	}
	all := append(append([]stats.Sample{}, prev...), cur...)
	from, to := t0, t0.Add(48*time.Hour)
	k := ComputeKPIs(all, insThr, from, to, time.UTC)
	if k.Count != 576 || !near(k.TIR, 100) || !near(k.Avg, 100) || !near(k.GMI, 3.31+0.02392*100) || k.Days != 2 || k.Coverage < 99 {
		t.Errorf("%+v", k)
	}
	pf, pt := PreviousPeriod(from, to)
	if !pf.Equal(from.Add(-(to.Sub(from)))) || !pt.Before(from) {
		t.Errorf("prev %v %v", pf, pt)
	}
	pk := ComputeKPIs(all, insThr, pf, pt, time.UTC)
	if pk.Count != 576 || !near(pk.TIR, 50) || !near(pk.Avg, 150) {
		t.Errorf("prev kpis %+v", pk)
	}
	d := DeltaKPIs(k, pk)
	if !d.Valid || !near(d.TIR, 50) || !near(d.Avg, -50) {
		t.Errorf("delta %+v", d)
	}
	if DeltaKPIs(k, KPIs{}).Valid || DeltaKPIs(KPIs{}, k).Valid {
		t.Errorf("delta with an empty period must be invalid")
	}
	if (ComputeKPIs(nil, insThr, from, to, time.UTC) != KPIs{}) {
		t.Errorf("empty kpis")
	}
}

func TestPreviousPeriodDoesNotDoubleCountBoundary(t *testing.T) {
	from := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	s := []stats.Sample{{Time: from, Value: 100}}
	pf, pt := PreviousPeriod(from, to)
	if n := ComputeKPIs(s, insThr, pf, pt, time.UTC).Count; n != 0 {
		t.Errorf("boundary reading counted in previous period: %d", n)
	}
	if n := ComputeKPIs(s, insThr, from, to, time.UTC).Count; n != 1 {
		t.Errorf("boundary reading missing from current period: %d", n)
	}
}

func TestCacheLRU(t *testing.T) {
	c := NewCache[string, int](2)
	c.Put("a", 1)
	c.Put("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 { // a becomes most recent
		t.Fatal("get a")
	}
	c.Put("c", 3) // evicts b
	if _, ok := c.Get("b"); ok {
		t.Errorf("b should be evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Errorf("a should survive")
	}
	c.Put("a", 10)
	if v, _ := c.Get("a"); v != 10 || c.Len() != 2 {
		t.Errorf("update %v len %d", v, c.Len())
	}
	if NewCache[int, int](0).max != 1 {
		t.Errorf("min size")
	}
}

func TestCacheDoAndConcurrency(t *testing.T) {
	c := NewCache[int, int](4)
	calls := 0
	for i := 0; i < 3; i++ {
		if v := c.Do(7, func() int { calls++; return 49 }); v != 49 {
			t.Fatal(v)
		}
	}
	if calls != 1 {
		t.Errorf("computed %d times", calls)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				c.Do(i%10, func() int { return i % 10 })
			}
		}()
	}
	wg.Wait()
	if c.Len() > 4 {
		t.Errorf("len %d", c.Len())
	}
}

func BenchmarkInsightsFor(b *testing.B) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	n := 288 * 365
	s := make([]stats.Sample, n)
	for i := range s {
		s[i] = stats.Sample{Time: t0.Add(time.Duration(i) * 5 * time.Minute), Value: 110 + 40*math.Sin(float64(i)/40)}
	}
	var acts []ActivityInput
	for d := 0; d < 300; d++ {
		st := t0.AddDate(0, 0, d).Add(7 * time.Hour)
		acts = append(acts, ActivityInput{Sport: "Run", Start: st, End: st.Add(45 * time.Minute)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		InsightsFor(s, acts, insThr, time.UTC)
	}
}
