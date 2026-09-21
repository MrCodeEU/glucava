package render

import (
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

func fixture(vals ...float64) ([]stats.Sample, stats.Summary) {
	t0 := time.Date(2026, 9, 20, 7, 0, 0, 0, time.UTC)
	s := make([]stats.Sample, len(vals))
	for i, v := range vals {
		s[i] = stats.Sample{Time: t0.Add(time.Duration(i) * 5 * time.Minute), Value: v}
	}
	sum, _ := stats.Summarize(s, stats.DefaultRange)
	return s, sum
}

func TestBlockMgdl(t *testing.T) {
	s, sum := fixture(100, 120, 140, 160)
	got := Block(sum, s, Options{SparkWidth: 4})
	want := "🩸 TIR 100% | min 100 | max 160 | avg 130 mg/dL\n▁▃▆█"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBlockMmol(t *testing.T) {
	s, sum := fixture(90.08, 180)
	got := Block(sum, s, Options{Unit: MmolL, SparkWidth: -1})
	want := "🩸 TIR 100% | min 5.0 | max 10.0 | avg 7.5 mmol/L"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMergeAppend(t *testing.T) {
	got := Merge("Nice morning run\n", "🩸 A\n▁")
	if got != "Nice morning run\n\n🩸 A\n▁" {
		t.Errorf("got %q", got)
	}
	if got := Merge("", "🩸 A"); got != "🩸 A" {
		t.Errorf("empty existing: got %q", got)
	}
}

func TestMergeReplaceKeepsOtherText(t *testing.T) {
	existing := "Before\n\n🩸 old\n▂▂\n\nAfter my text"
	got := Merge(existing, "🩸 new\n▁█")
	want := "Before\n\n🩸 new\n▁█\n\nAfter my text"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMergeReplaceAtEnd(t *testing.T) {
	got := Merge("Run\n\n🩸 old\n▂", "🩸 new")
	if got != "Run\n\n🩸 new" {
		t.Errorf("got %q", got)
	}
}

func TestMergeIdempotent(t *testing.T) {
	block := "🩸 x\n▁▂"
	once := Merge("Notes", block)
	twice := Merge(once, block)
	if once != twice {
		t.Errorf("not idempotent:\n%q\n%q", once, twice)
	}
	if strings.Count(twice, Prefix) != 1 {
		t.Errorf("block duplicated: %q", twice)
	}
}

func TestMergeCRLF(t *testing.T) {
	got := Merge("Run\r\n\r\n🩸 old\r\n▂\r\n", "🩸 new")
	if got != "Run\n\n🩸 new\n" {
		t.Errorf("got %q", got)
	}
}

func TestStrip(t *testing.T) {
	block := Prefix + "TIR 90% | min 70 | max 150 | avg 100 mg/dL\n▁▂▃"
	for name, tc := range map[string]struct{ in, want string }{
		"empty":       {"", ""},
		"no block":    {"My run  \n", "My run"},
		"only block":  {block, ""},
		"after text":  {"My run\n\n" + block, "My run"},
		"before text": {block + "\n\nafter", "after"},
		"crlf":        {"My run\r\n\r\n" + block, "My run"},
		"idempotent":  {Merge("hello", block), "hello"},
	} {
		if got := Strip(tc.in); got != tc.want {
			t.Errorf("%s: Strip = %q, want %q", name, got, tc.want)
		}
	}
}
