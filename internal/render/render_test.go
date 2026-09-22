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
	got := Merge("Nice morning run\n", "🩸 TIR 100% | min 1 | max 1 | avg 1 mg/dL\n▁")
	if got != "Nice morning run\n\n🩸 TIR 100% | min 1 | max 1 | avg 1 mg/dL\n▁" {
		t.Errorf("got %q", got)
	}
	if got := Merge("", "🩸 TIR 100% | min 1 | max 1 | avg 1 mg/dL"); got != "🩸 TIR 100% | min 1 | max 1 | avg 1 mg/dL" {
		t.Errorf("empty existing: got %q", got)
	}
}

func TestMergeReplaceKeepsOtherText(t *testing.T) {
	existing := "Before\n\n🩸 TIR 1% | min 1 | max 1 | avg 1 mg/dL\n▂▂\n\nAfter my text"
	got := Merge(existing, "🩸 TIR 2% | min 2 | max 2 | avg 2 mg/dL\n▁█")
	want := "Before\n\n🩸 TIR 2% | min 2 | max 2 | avg 2 mg/dL\n▁█\n\nAfter my text"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMergeReplaceAtEnd(t *testing.T) {
	got := Merge("Run\n\n🩸 TIR 1% | min 1 | max 1 | avg 1 mg/dL\n▂", "🩸 TIR 2% | min 2 | max 2 | avg 2 mg/dL")
	if got != "Run\n\n🩸 TIR 2% | min 2 | max 2 | avg 2 mg/dL" {
		t.Errorf("got %q", got)
	}
}

func TestMergeIdempotent(t *testing.T) {
	block := "🩸 TIR 1% | min 1 | max 1 | avg 1 mg/dL\n▁▂"
	once := Merge("Notes", block)
	twice := Merge(once, block)
	if once != twice {
		t.Errorf("not idempotent:\n%q\n%q", once, twice)
	}
	if strings.Count(twice, blockMarker) != 1 {
		t.Errorf("block duplicated: %q", twice)
	}
}

func TestMergeCRLF(t *testing.T) {
	got := Merge("Run\r\n\r\n🩸 TIR 1% | min 1 | max 1 | avg 1 mg/dL\r\n▂\r\n", "🩸 TIR 2% | min 2 | max 2 | avg 2 mg/dL")
	if got != "Run\n\n🩸 TIR 2% | min 2 | max 2 | avg 2 mg/dL\n" {
		t.Errorf("got %q", got)
	}
}

// TestMergeDoesNotCollideWithAnotherAppsSameEmoji is a regression test: a
// real-world case (Ando, another Strava/Dexcom integration) writes its own
// summary starting with the same 🩸 emoji ("🩸 Avg : ..."). Merge must never
// mistake that for a previous Glucava block, or repeated runs either eat
// someone else's text or, worse, never find their own block to replace and
// pile up a fresh copy every time instead.
func TestMergeDoesNotCollideWithAnotherAppsSameEmoji(t *testing.T) {
	foreign := "🎯 75% in Range\n🩸 Avg : 156 - Min : 124 - Max : 201 (mg/dL)\n🔗 https://app.ando.care/activity/1"
	block := "🩸 TIR 78% | min 122 | max 202 | avg 153 mg/dL\n▅▇███"

	once := Merge(foreign, block)
	if !strings.Contains(once, "Avg : 156") || !strings.Contains(once, "ando.care") {
		t.Fatalf("foreign block was eaten: %q", once)
	}
	if strings.Count(once, blockMarker) != 1 {
		t.Fatalf("own block not inserted exactly once: %q", once)
	}

	twice := Merge(once, block)
	if once != twice {
		t.Errorf("not idempotent alongside foreign text:\n%q\n%q", once, twice)
	}
	if strings.Count(twice, blockMarker) != 1 {
		t.Errorf("reprocessing duplicated the block: %q", twice)
	}
	if !strings.Contains(twice, "ando.care") {
		t.Errorf("foreign block lost on the second merge: %q", twice)
	}
}

// TestMergeSelfHealsWhenSavedWithoutBlankLines replicates what was actually
// observed on strava.com: after a save, re-fetching the description came
// back with the blank line between the old block and the rest collapsed to a
// single newline. A boundary based on "next blank line" then never finds the
// end of its own block, and every reprocess just appends another copy nothing
// ever replaces. Merge must bound its own block by shape (its known line
// count), not by a blank line, so this converges instead of accumulating.
func TestMergeSelfHealsWhenSavedWithoutBlankLines(t *testing.T) {
	block := "🩸 TIR 78% | min 122 | max 202 | avg 153 mg/dL\n▅▇███"
	// Three copies of the same block already glued together with single
	// newlines (no blank line anywhere), as found on a real account.
	corrupted := block + "\n" + block + "\n" + block
	got := Merge(corrupted, block)
	if strings.Count(got, blockMarker) != 1 {
		t.Errorf("did not collapse duplicates: %q", got)
	}
	if got != block {
		t.Errorf("got %q, want just one block %q", got, block)
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
