package store

import (
	"context"
	"testing"
	"time"
)

func TestArtifactMarksRoundTrip(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	v0, err := s.DataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}

	a, b := t0, t0.Add(30*time.Minute)
	if err := s.AddArtifactMark(ctx, a, b, MarkArtifact); err != nil {
		t.Fatal(err)
	}
	if err := s.AddArtifactMark(ctx, a.Add(10*time.Minute), a.Add(20*time.Minute), MarkReal); err != nil {
		t.Fatal(err)
	}
	if err := s.AddArtifactMark(ctx, t0.Add(48*time.Hour), t0.Add(49*time.Hour), MarkArtifact); err != nil {
		t.Fatal(err)
	}

	got, err := s.ArtifactMarks(ctx, t0.Add(-time.Hour), t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != MarkArtifact || got[1].Kind != MarkReal {
		t.Fatalf("marks in window = %+v (want artifact then real, oldest first)", got)
	}
	if !got[0].Start.Equal(a) || !got[0].End.Equal(b) {
		t.Errorf("span = %v..%v", got[0].Start, got[0].End)
	}

	if none, err := s.ArtifactMarks(ctx, t0.Add(10*time.Hour), t0.Add(11*time.Hour)); err != nil || len(none) != 0 {
		t.Errorf("empty window = %v, %v", none, err)
	}

	v1, err := s.DataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v1 == v0 || v1.Marks != 3 {
		t.Errorf("a new mark must change the data version: %+v -> %+v", v0, v1)
	}
}

func TestArtifactMarkValidation(t *testing.T) {
	s := &PB{App: newApp(t)}
	ctx := context.Background()
	if err := s.AddArtifactMark(ctx, t0, t0.Add(time.Minute), "maybe"); err == nil {
		t.Error("unknown kind accepted")
	}
	if err := s.AddArtifactMark(ctx, t0.Add(time.Minute), t0, MarkArtifact); err == nil {
		t.Error("end before start accepted")
	}
}

func TestLanguageConfig(t *testing.T) {
	s := &PB{App: newApp(t)}
	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "auto" {
		t.Errorf("default language = %q, want auto", cfg.Language)
	}
	cfg.Language = "de"
	if msg := cfg.Validate(); msg != "" {
		t.Fatalf("valid language rejected: %s", msg)
	}
	if err := s.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	back, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if back.Language != "de" {
		t.Errorf("language after round trip = %q", back.Language)
	}
	back.Language = "xx-nope"
	if back.Validate() == "" {
		t.Error("unknown language accepted")
	}
}

func TestArtifactModeConfig(t *testing.T) {
	s := &PB{App: newApp(t)}
	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ArtifactsMode() != ArtifactFlagged {
		t.Errorf("default mode = %q, want flagged", cfg.ArtifactsMode())
	}
	cfg.ArtifactMode = ArtifactExclude
	if msg := cfg.Validate(); msg != "" {
		t.Fatalf("valid mode rejected: %s", msg)
	}
	if err := s.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	back, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if back.ArtifactsMode() != ArtifactExclude {
		t.Errorf("mode after round trip = %q", back.ArtifactMode)
	}
	back.ArtifactMode = "bogus"
	if back.Validate() == "" {
		t.Error("bogus mode accepted")
	}
}
