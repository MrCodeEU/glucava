package analytics

import (
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

func bigSeries(n int) []stats.Sample {
	s := make([]stats.Sample, n)
	start := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range s {
		v := 60 + float64((i*37)%200)
		s[i] = stats.Sample{Time: start.Add(time.Duration(i) * 5 * time.Minute), Value: v}
	}
	return s
}

func BenchmarkAll100k(b *testing.B) {
	s := bigSeries(100_000)
	from, to := s[0].Time, s[len(s)-1].Time
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ComputeTIR5(s, th)
		_ = ComputeAGP(s, vie, 0)
		_ = ComputeCoverage(s, from, to, 0, 0)
		_ = DetectEpisodes(s, th, EpisodeOptions{Loc: vie})
		_ = ComputeDayParts(s, th, vie)
		_ = ComputeWeekdayHour(s, th, vie)
		_ = ComputeDaily(s, th, vie)
	}
}

func BenchmarkAGP100k(b *testing.B) {
	s := bigSeries(100_000)
	for i := 0; i < b.N; i++ {
		_ = ComputeAGP(s, vie, 0)
	}
}

func BenchmarkEpisodes100k(b *testing.B) {
	s := bigSeries(100_000)
	for i := 0; i < b.N; i++ {
		_ = DetectEpisodes(s, th, EpisodeOptions{Loc: vie})
	}
}
