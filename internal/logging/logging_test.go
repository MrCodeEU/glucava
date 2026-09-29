package logging

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/MrCodeEU/glucava/internal/bus"
)

func TestHandleAppendsAndForwards(t *testing.T) {
	var out bytes.Buffer
	b := &bus.Bus{}
	woke, cancel := b.Subscribe()
	defer cancel()

	h := NewHandler(slog.NewTextHandler(&out, nil), 10, b)
	logger := slog.New(h)
	logger.Info("hello", "activity", "123")

	if out.Len() == 0 {
		t.Error("nothing forwarded to the underlying handler")
	}
	select {
	case <-woke:
	default:
		t.Error("bus was not published after Handle")
	}

	recent := h.Recent(10)
	if len(recent) != 1 {
		t.Fatalf("Recent = %d entries, want 1", len(recent))
	}
	if recent[0].Message != "hello" || recent[0].Level != slog.LevelInfo || recent[0].Attrs != "activity=123" {
		t.Errorf("entry = %+v", recent[0])
	}
}

func TestRecentIsNewestFirstAndCapped(t *testing.T) {
	h := NewHandler(slog.NewTextHandler(bytes.NewBuffer(nil), nil), 2, nil)
	logger := slog.New(h)
	logger.Info("first")
	logger.Info("second")
	logger.Info("third") // pushes "first" out of the 2-entry ring buffer

	recent := h.Recent(10)
	if len(recent) != 2 {
		t.Fatalf("Recent = %d entries, want 2 (capacity)", len(recent))
	}
	if recent[0].Message != "third" || recent[1].Message != "second" {
		t.Errorf("order = %q, %q, want third, second (newest first)", recent[0].Message, recent[1].Message)
	}
}

func TestWithAttrsSharesTheSameBuffer(t *testing.T) {
	h := NewHandler(slog.NewTextHandler(bytes.NewBuffer(nil), nil), 10, nil)
	logger := slog.New(h).With("component", "test")
	logger.Info("via With")

	if len(h.Recent(10)) != 1 {
		t.Error("entry logged through a With()'d logger did not land in the parent Handler's buffer")
	}
}
