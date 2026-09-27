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
	want := sentinel + "🩸 TIR 100% | min 100 | max 160 | avg 130 mg/dL\n▁▃▆█" + endSentinel
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBlockMmol(t *testing.T) {
	s, sum := fixture(90.08, 180)
	got := Block(sum, s, Options{Unit: MmolL, SparkWidth: -1})
	want := sentinel + "🩸 TIR 100% | min 5.0 | max 10.0 | avg 7.5 mmol/L" + endSentinel
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
	if strings.Count(twice, legacyBlockMarker) != 1 {
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
// real-world case (another Strava/Dexcom integration) writes its own
// summary starting with the same 🩸 emoji ("🩸 Avg : ..."). Merge must never
// mistake that for a previous Glucava block, or repeated runs either eat
// someone else's text or, worse, never find their own block to replace and
// pile up a fresh copy every time instead.
func TestMergeDoesNotCollideWithAnotherAppsSameEmoji(t *testing.T) {
	foreign := "🎯 75% in Range\n🩸 Avg : 156 - Min : 124 - Max : 201 (mg/dL)\n🔗 https://other-app.example/activity/1"
	block := "🩸 TIR 78% | min 122 | max 202 | avg 153 mg/dL\n▅▇███"

	once := Merge(foreign, block)
	if !strings.Contains(once, "Avg : 156") || !strings.Contains(once, "other-app.example") {
		t.Fatalf("foreign block was eaten: %q", once)
	}
	if strings.Count(once, legacyBlockMarker) != 1 {
		t.Fatalf("own block not inserted exactly once: %q", once)
	}

	twice := Merge(once, block)
	if once != twice {
		t.Errorf("not idempotent alongside foreign text:\n%q\n%q", once, twice)
	}
	if strings.Count(twice, legacyBlockMarker) != 1 {
		t.Errorf("reprocessing duplicated the block: %q", twice)
	}
	if !strings.Contains(twice, "other-app.example") {
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
	if strings.Count(got, legacyBlockMarker) != 1 {
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

func TestPreservesText(t *testing.T) {
	user := "Easy 10k\n\nfelt great"
	block := "🩸 TIR 90% | min 70 | max 150 | avg 110 mg/dL\n▁▂▃"
	merged := Merge(user, block)
	if !PreservesText(user, merged) {
		t.Errorf("a normal merge must preserve the user's text: %q", merged)
	}
	if !PreservesText(merged, Merge(merged, "🩸 TIR 80% | min 60 | max 190 | avg 120 mg/dL")) {
		t.Error("re-merging must preserve the user's text")
	}
	otherApp := "🩸 Avg : 5.9 mmol/L\n🩸 Time in range : 90%"
	if !PreservesText(otherApp, Merge(otherApp, block)) {
		t.Error("another app's 🩸 lines must be preserved, not mistaken for ours")
	}
	for name, bad := range map[string]string{
		"user text lost":    block,
		"user text changed": "Easy 5k\n\nfelt great\n\n" + block,
		"text appended":     merged + "\nextra",
	} {
		if PreservesText(user, bad) {
			t.Errorf("%s: expected the guard to fire", name)
		}
	}
}

// TestMergeUpgradesLegacyBlockToSentinel is the real-world path for every
// activity already annotated by 0.1.2 or earlier: its block on Strava has no
// sentinel yet. The next reprocess must recognise it via legacyBlockMarker,
// replace it in place, and leave exactly one block behind — the new,
// sentinel-carrying one — not a second copy next to the old text.
func TestMergeUpgradesLegacyBlockToSentinel(t *testing.T) {
	s, sum := fixture(100, 120, 140, 160)
	newBlock := Block(sum, s, Options{SparkWidth: -1})
	legacy := "Before\n\n🩸 TIR 1% | min 1 | max 1 | avg 1 mg/dL\n▂▂\n\nAfter my text"

	got := Merge(legacy, newBlock)
	want := "Before\n\n" + newBlock + "\n\nAfter my text"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Count(got, blockMarker) != 1 || strings.Count(got, legacyBlockMarker) != 1 {
		// blockMarker is just the sentinel now; legacyBlockMarker is the
		// visible "🩸 TIR " text the default template still writes right
		// after it, so a single upgraded line legitimately counts as 1 for
		// both.
		t.Errorf("expected exactly one block after the upgrade: %q", got)
	}

	// Reprocessing again must be idempotent and still exactly one block.
	again := Merge(got, newBlock)
	if again != got {
		t.Errorf("not idempotent after the upgrade:\n%q\n%q", got, again)
	}
}

// TestSentinelIsInvisibleAndUnique documents what the sentinel is for: two
// code points that render as nothing, so a human reading the activity sees
// the same text as before, but a coincidental duplicate is effectively
// impossible.
func TestSentinelIsInvisibleAndUnique(t *testing.T) {
	if len([]rune(sentinel)) != 2 {
		t.Fatalf("sentinel = %d code points, want 2", len([]rune(sentinel)))
	}
	for _, r := range sentinel {
		if r != '​' && r != '⁠' {
			t.Errorf("unexpected code point in sentinel: U+%04X", r)
		}
	}
	s, sum := fixture(100, 120, 140, 160)
	block := Block(sum, s, Options{SparkWidth: -1})
	if !strings.HasPrefix(block, sentinel) {
		t.Errorf("Block does not start with the sentinel: %q", block)
	}
	if !strings.HasPrefix(strings.TrimPrefix(block, sentinel), Prefix) {
		t.Errorf("the visible text right after the sentinel changed: %q", block)
	}
}

// TestMergeDoesNotEatUserLineThatLooksLikeASparkline is the concrete gap
// endSentinel closes: without it, a block's end was only ever guessed from
// shape, so a coincidence in the user's own very next line (one made only of
// the same block-drawing characters a real sparkline uses) would be
// mistaken for part of the block and eaten by Strip/Merge, even though
// Block() never actually attached it.
func TestMergeDoesNotEatUserLineThatLooksLikeASparkline(t *testing.T) {
	s, sum := fixture(100, 120, 140, 160)
	block := Block(sum, s, Options{SparkWidth: -1}) // no real sparkline from Block itself
	existing := block + "\n▁▂▃ my own progress bar, not a sparkline"

	stripped := Strip(existing)
	if stripped != "▁▂▃ my own progress bar, not a sparkline" {
		t.Fatalf("the user's own line was eaten: %q", stripped)
	}

	newBlock := Block(sum, s, Options{SparkWidth: -1})
	merged := Merge(existing, newBlock)
	if !strings.Contains(merged, "my own progress bar") {
		t.Errorf("the user's own line was lost on merge: %q", merged)
	}
	if strings.Count(merged, blockMarker) != 1 {
		t.Errorf("expected exactly one block: %q", merged)
	}
}

// TestRemoveBlocksNeverScansPastTheSecondLineWithoutAnEndSentinel guards the
// safety property endSentinel relies on: a block that somehow lost its end
// marker (case not currently reachable through Block(), but the bound must
// hold regardless) must never eat more than one line past its start, so a
// missing end sentinel can only under-bound a block, never run away through
// the rest of the user's text.
func TestRemoveBlocksNeverScansPastTheSecondLineWithoutAnEndSentinel(t *testing.T) {
	broken := blockMarker + "1% | min 1 | max 1 | avg 1 mg/dL" // no endSentinel anywhere
	existing := broken + "\nsome text\nmore text\neven more"
	got := Strip(existing)
	if got != "some text\nmore text\neven more" {
		t.Errorf("scanned past the first line: %q", got)
	}
}

// TestMergeRecognisesACustomWordedBlock is the prerequisite a free-form
// description template needs: detection must not depend on the block's
// visible text starting with "TIR ". A block written with entirely
// different wording, but still carrying the sentinel, must still be found
// and replaced on the next merge, not duplicated next to itself.
func TestMergeRecognisesACustomWordedBlock(t *testing.T) {
	custom := sentinel + "Range 88%, avg 130, that's it" + endSentinel
	existing := "My run notes.\n\n" + custom
	if !hasBlockPrefix(custom) {
		t.Fatalf("custom-worded block not recognised: %q", custom)
	}

	replacement := sentinel + "Range 90%, avg 128, that's it" + endSentinel
	got := Merge(existing, replacement)
	want := "My run notes.\n\n" + replacement
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Count(got, sentinel) != 1 {
		t.Errorf("expected exactly one block: %q", got)
	}
}

// TestDefaultTemplateMatchesBlock is the guard for the template engine's
// core promise: DefaultTemplate must reproduce Block()'s exact output,
// byte for byte, for both units and with and without a sparkline. Anyone
// switching a deployment from the old hardcoded Block() onto the new
// template path (with DefaultTemplate) must see no change on Strava.
func TestDefaultTemplateMatchesBlock(t *testing.T) {
	s, sum := fixture(60, 70, 100, 180, 200)
	for _, opt := range []Options{
		{Unit: MgDL},
		{Unit: MmolL},
		{Unit: MgDL, SparkWidth: -1},
		{Unit: MgDL, SparkWidth: 4},
	} {
		want := Block(sum, s, opt)
		got, err := RenderBlock(DefaultTemplate, sum, s, opt)
		if err != nil {
			t.Fatalf("opt=%+v: %v", opt, err)
		}
		if got != want {
			t.Errorf("opt=%+v:\ngot  %q\nwant %q", opt, got, want)
		}
	}
}

// TestRenderBlockCustomTemplate exercises a template that uses fields
// outside the default wording (GMI, StdDev, round()), to prove the engine
// is not just able to reproduce the default.
func TestRenderBlockCustomTemplate(t *testing.T) {
	s, sum := fixture(60, 70, 100, 180, 200)
	tmpl := `GMI {{.GMI}}% | stddev {{.StdDev}} | avg {{round .Sum.Avg 1}}`
	got, err := RenderBlock(tmpl, sum, s, Options{Unit: MgDL, SparkWidth: -1})
	if err != nil {
		t.Fatal(err)
	}
	want := sentinel + "GMI 6% | stddev 57 | avg 122.0" + endSentinel
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestPresetsAllRender guards every built-in preset: each must parse and
// execute against real sample data (fixture), and none may collide on ID.
func TestPresetsAllRender(t *testing.T) {
	s, sum := fixture(60, 70, 100, 180, 200)
	seen := map[string]bool{}
	for _, p := range Presets {
		if seen[p.ID] {
			t.Errorf("duplicate preset id %q", p.ID)
		}
		seen[p.ID] = true
		if _, err := RenderBlock(p.Template, sum, s, Options{Unit: MgDL}); err != nil {
			t.Errorf("preset %q: %v", p.ID, err)
		}
	}
}

// TestDefaultPresetMatchesDefaultTemplate is the guarantee that picking the
// "default" preset changes nothing: it must be exactly DefaultTemplate, not
// just render the same output as it.
func TestDefaultPresetMatchesDefaultTemplate(t *testing.T) {
	p, ok := PresetByID("default")
	if !ok {
		t.Fatal(`no "default" preset`)
	}
	if p.Template != DefaultTemplate {
		t.Errorf("default preset's template != DefaultTemplate")
	}
}

func TestPresetByIDUnknown(t *testing.T) {
	if _, ok := PresetByID("nope"); ok {
		t.Error("expected ok=false for an unknown preset id")
	}
}

// TestRenderBlockBadTemplateErrors documents that a broken template fails
// loudly (parse or execute error) rather than silently producing garbage
// that would then get written to Strava.
func TestRenderBlockBadTemplateErrors(t *testing.T) {
	s, sum := fixture(60, 70, 100, 180, 200)
	if _, err := RenderBlock("{{.NoSuchField}}", sum, s, Options{}); err == nil {
		t.Error("expected an error for an unknown field")
	}
	if _, err := RenderBlock("{{.TIR", sum, s, Options{}); err == nil {
		t.Error("expected an error for unclosed template syntax")
	}
}
