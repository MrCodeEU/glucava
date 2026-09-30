package analytics

import (
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// night is 02:00 UTC, which is night in UTC and in Vienna.
var night = time.Date(2026, 9, 20, 2, 0, 0, 0, time.UTC)

func kinds(as []Artifact) []ArtifactKind {
	var k []ArtifactKind
	for _, a := range as {
		k = append(k, a.Kind)
	}
	return k
}

func flat(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func cat(parts ...[]float64) []float64 {
	var out []float64
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestDetectArtifactsTable(t *testing.T) {
	noon := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		start time.Time
		vals  []float64
		want  []ArtifactKind
	}{
		{"overnight compression low", night, cat(flat(4, 105), []float64{62, 55, 52, 58, 100}, flat(4, 105)), []ArtifactKind{ArtifactCompression}},
		{"compression 60 min", night, cat(flat(3, 105), []float64{60}, flat(11, 55), []float64{100}, flat(3, 105)), []ArtifactKind{ArtifactCompression}},
		{"same shape by day is only a dip", noon, cat(flat(4, 105), []float64{62, 55, 52, 58, 100}, flat(4, 105)), []ArtifactKind{ArtifactDip}},
		{"dip 30 mg/dL for 10 min", noon, cat(flat(4, 130), []float64{98, 96}, flat(5, 130)), []ArtifactKind{ArtifactDip}},
		{"gradual fall is not a dip", noon, cat(flat(4, 130), []float64{120, 110, 100, 95, 100, 110, 120, 130}, flat(3, 130)), nil},
		{"genuine slow low", night, []float64{120, 114, 108, 102, 96, 90, 84, 78, 72, 66, 62, 60, 62, 66, 72, 78, 84, 90, 96, 100}, nil},
		{"genuine fast but sustained low", night, cat(flat(3, 110), []float64{80, 58}, flat(18, 55), []float64{70, 90, 105}, flat(3, 105)), nil},
		{"genuine low treated, slow recovery", night, []float64{110, 95, 80, 65, 58, 55, 56, 62, 70, 80, 90, 100, 105}, nil},
		{"low lasting over an hour is real", night, cat(flat(3, 105), []float64{60}, flat(14, 55), []float64{100}, flat(3, 105)), nil},
		{"onset just under the rate", night, cat(flat(2, 105), []float64{96, 87, 78, 69, 60, 51, 100}, flat(3, 105)), nil},
		{"drop just under 25", noon, cat(flat(4, 130), []float64{106, 106}, flat(5, 130)), nil},
		{"drop of exactly 25", noon, cat(flat(4, 130), []float64{105, 105}, flat(5, 130)), []ArtifactKind{ArtifactDip}},
		{"in range wobble", noon, []float64{100, 105, 98, 110, 102, 99, 104}, nil},
		{"no reading before the low", night, []float64{60, 55, 100, 105}, nil},
		{"data ends inside the low", night, cat(flat(3, 105), []float64{60, 55, 52}), nil},
	} {
		got := DetectArtifacts(series(tc.start, 5*time.Minute, tc.vals...), th, ArtifactOptions{})
		gk := kinds(got)
		if len(gk) != len(tc.want) {
			t.Errorf("%s: kinds %v, want %v (%+v)", tc.name, gk, tc.want, got)
			continue
		}
		for i := range gk {
			if gk[i] != tc.want[i] {
				t.Errorf("%s: kinds %v, want %v", tc.name, gk, tc.want)
			}
		}
	}
}

func TestDipAcrossSignalGap(t *testing.T) {
	// 132, then 98 (fall of 34), a 12 minute silence, then back at 128.
	s := []stats.Sample{
		{Time: night, Value: 130}, {Time: night.Add(5 * time.Minute), Value: 132},
		{Time: night.Add(10 * time.Minute), Value: 98},
		{Time: night.Add(22 * time.Minute), Value: 128},
		{Time: night.Add(27 * time.Minute), Value: 130},
	}
	got := DetectArtifacts(s, th, ArtifactOptions{})
	if len(got) != 1 || got[0].Kind != ArtifactDip {
		t.Fatalf("got %+v", got)
	}
	a := got[0]
	if !a.Start.Equal(s[2].Time) || !a.End.Equal(s[2].Time) || a.Nadir != 98 || a.PreLevel != 132 || a.PostLevel != 128 {
		t.Errorf("span %+v", a)
	}
	if a.Confidence < 0.9 {
		t.Errorf("gap + full recovery + big drop should be confident, got %v", a.Confidence)
	}
}

func TestCompressionFields(t *testing.T) {
	s := series(night, 5*time.Minute, 105, 105, 105, 62, 55, 52, 58, 100, 105)
	got := DetectArtifacts(s, th, ArtifactOptions{})
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	a := got[0]
	if a.Kind != ArtifactCompression || !a.Start.Equal(s[3].Time) || !a.End.Equal(s[6].Time) || a.Nadir != 52 || a.PreLevel != 105 || a.PostLevel != 100 {
		t.Errorf("span %+v", a)
	}
	if a.Reason == "" || a.Confidence < 0.5 || a.Confidence > 1 {
		t.Errorf("reason/confidence %+v", a)
	}
	if len(DetectArtifacts(s, th, ArtifactOptions{MinConfidence: 1.01})) != 0 {
		t.Error("MinConfidence not applied")
	}
}

func TestCompressionNightWindowAndDST(t *testing.T) {
	// 2026-10-25 is the end of DST in Vienna: 02:00-03:00 happens twice.
	// 00:30 UTC is 02:30 CEST, still night; 08:30 UTC is 09:30 CET, morning.
	vals := []float64{105, 105, 105, 62, 55, 52, 58, 100, 105}
	dstNight := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	if got := DetectArtifacts(series(dstNight, 5*time.Minute, vals...), th, ArtifactOptions{Loc: vie}); len(got) != 1 || got[0].Kind != ArtifactCompression {
		t.Errorf("DST night: %+v", got)
	}
	morning := time.Date(2026, 10, 25, 8, 30, 0, 0, time.UTC)
	if got := DetectArtifacts(series(morning, 5*time.Minute, vals...), th, ArtifactOptions{Loc: vie}); len(got) != 1 || got[0].Kind != ArtifactDip {
		t.Errorf("morning must not be compression: %+v", got)
	}
	utcEvening := time.Date(2026, 9, 20, 21, 0, 0, 0, time.UTC) // 23:00 in Vienna
	if got := DetectArtifacts(series(utcEvening, 5*time.Minute, vals...), th, ArtifactOptions{Loc: vie}); len(got) != 1 || got[0].Kind != ArtifactCompression {
		t.Errorf("23:00 Vienna: %+v", got)
	}
	if got := DetectArtifacts(series(night, 5*time.Minute, vals...), th, ArtifactOptions{NightStartHour: 3, NightEndHour: 6}); len(got) != 1 || got[0].Kind != ArtifactDip {
		t.Errorf("02:00 outside a 03-06 window: %+v", got)
	}
}

func TestExcludeArtifacts(t *testing.T) {
	s := series(night, 5*time.Minute, 105, 105, 105, 62, 55, 52, 58, 100, 105)
	spans := DetectArtifacts(s, th, ArtifactOptions{})
	clean := ExcludeArtifacts(s, spans)
	if len(clean) != len(s)-4 {
		t.Fatalf("len %d, want %d", len(clean), len(s)-4)
	}
	for _, x := range clean {
		if x.Value < 70 {
			t.Errorf("a low survived: %+v", x)
		}
	}
	if len(s) != 9 {
		t.Error("input modified")
	}
	if got := ExcludeArtifacts(s, nil); len(got) != len(s) {
		t.Error("no spans should be a no-op")
	}
	if eps := OfKind(DetectEpisodes(clean, th, EpisodeOptions{}), KindLow); len(eps) != 0 {
		t.Errorf("cleaned data still has a low episode: %+v", eps)
	}
}

func TestApplyMarks(t *testing.T) {
	s := series(night, 5*time.Minute, 105, 105, 105, 62, 55, 52, 58, 100, 105)
	spans := DetectArtifacts(s, th, ArtifactOptions{})
	if len(spans) != 1 {
		t.Fatal(spans)
	}
	if got := ApplyMarks(spans, []Mark{{Start: s[4].Time, End: s[5].Time, Kind: MarkReal}}, s); len(got) != 0 {
		t.Errorf("real mark: %+v", got)
	}
	got := ApplyMarks(spans, []Mark{{Start: s[3].Time, End: s[6].Time, Kind: MarkArtifact}}, s)
	if len(got) != 1 || !got[0].Marked || got[0].Confidence != 1 {
		t.Errorf("artifact on detected: %+v", got)
	}
	if spans[0].Marked {
		t.Error("ApplyMarks modified its input")
	}
	slow := series(night, 5*time.Minute, 120, 110, 100, 90, 80, 70, 65, 60, 62, 66, 72, 80)
	if len(DetectArtifacts(slow, th, ArtifactOptions{})) != 0 {
		t.Fatal("slow low should not be flagged")
	}
	got = ApplyMarks(nil, []Mark{{Start: slow[6].Time, End: slow[9].Time, Kind: MarkArtifact}}, slow)
	if len(got) != 1 || got[0].Kind != ArtifactMarked || got[0].Nadir != 60 || got[0].PreLevel != 70 || got[0].PostLevel != 72 {
		t.Errorf("new marked span: %+v", got)
	}
	// Later marks win.
	got = ApplyMarks(nil, []Mark{
		{Start: slow[6].Time, End: slow[9].Time, Kind: MarkArtifact},
		{Start: slow[7].Time, End: slow[7].Time, Kind: MarkReal},
	}, slow)
	if len(got) != 0 {
		t.Errorf("later real mark: %+v", got)
	}
}

func TestOverlappingArtifact(t *testing.T) {
	a := Artifact{Start: night, End: night.Add(20 * time.Minute)}
	if _, ok := OverlappingArtifact([]Artifact{a}, night.Add(20*time.Minute), night.Add(time.Hour)); !ok {
		t.Error("touching end should overlap")
	}
	if _, ok := OverlappingArtifact([]Artifact{a}, night.Add(21*time.Minute), night.Add(time.Hour)); ok {
		t.Error("later window should not overlap")
	}
}

func TestDetectArtifactsUnsortedAndEmpty(t *testing.T) {
	if got := DetectArtifacts(nil, th, ArtifactOptions{}); len(got) != 0 {
		t.Errorf("nil: %+v", got)
	}
	s := series(night, 5*time.Minute, 105, 105, 105, 62, 55, 52, 58, 100, 105)
	s[2], s[6] = s[6], s[2]
	if got := DetectArtifacts(s, th, ArtifactOptions{}); len(got) != 1 {
		t.Errorf("unsorted: %+v", got)
	}
}
