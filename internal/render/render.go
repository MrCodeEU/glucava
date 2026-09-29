// Package render builds the Strava description block and merges it into existing text.
package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// Prefix starts the visible first line of the block, shown to the reader.
const Prefix = "🩸 "

// sentinel is an invisible fingerprint (zero width space + word joiner)
// Block() puts at the very start of its line, before Prefix. It renders as
// nothing, so it changes nothing the reader sees, but it does not depend on
// wording or emoji another app could plausibly also use — unlike the emoji
// alone, or even "🩸 TIR ", both of which are just text a copycat or a
// coincidence could reproduce. Two invisible code points together, in this
// exact order, at the exact start of a line, are not something any other app
// is going to emit by accident.
// Written as explicit UTF-8 bytes (U+200B, then U+2060), not \u escapes or
// the literal characters, so the source carries no raw invisible code point
// for gofmt to silently rewrite it back into.
const sentinel = "\xe2\x80\x8b\xe2\x81\xa0"

// endSentinel closes a Glucava block: two different invisible code points
// (U+2062 INVISIBLE TIMES, U+2064 INVISIBLE PLUS), appended with no newline
// right after the block's own last visible character. removeBlocks matches
// on it to find exactly where a block ends, rather than guessing from shape
// (a line that merely "looks like" a sparkline) — closing the one real gap
// in the shape heuristic: a coincidence in the user's own next line. Also
// written as explicit UTF-8 bytes, for the same reason as sentinel above.
const endSentinel = "\xe2\x81\xa2\xe2\x81\xa4"

// blockMarker is what Merge and Strip match to recognise a Glucava block
// written by this version. It is the invisible sentinel alone, not the
// visible "TIR " text after it: a custom description template can start
// with different words, and detection must still find it, or a reprocess
// would never replace it and blocks would pile up on every run.
const blockMarker = sentinel

// legacyBlockMarker matches a block written before the sentinel existed
// (0.1.2 and earlier: text only, no invisible fingerprint). Matching it too
// means an activity processed by an older version is still recognised and
// upgraded to the new marker the next time it is reprocessed, rather than
// getting a second block appended beside the one already on Strava.
const legacyBlockMarker = Prefix + "TIR "

func hasBlockPrefix(l string) bool {
	return strings.HasPrefix(l, blockMarker) || strings.HasPrefix(l, legacyBlockMarker)
}

// Unit is the glucose display unit.
type Unit string

const (
	MgDL   Unit = "mg/dL"
	MmolL  Unit = "mmol/L"
	sparkW      = 24
)

// Options control block formatting.
type Options struct {
	Unit       Unit
	SparkWidth int // 0 means default; negative disables the sparkline

	// Sport, Distance (meters) and Duration back {{.Distance}}, {{.Elevation}}
	// and {{.Pace}}. Zero values (the default, e.g. in a template dry-run
	// with no real activity) mean those fields render empty, same as an
	// unset Sparkline.
	Sport         string
	Distance      float64 // meters
	ElevationGain float64 // meters
	Duration      time.Duration
}

// Block returns the description block: a summary line and an optional sparkline.
// It contains no blank lines, so Merge can find its end.
func Block(sum stats.Summary, samples []stats.Sample, opt Options) string {
	if opt.Unit == "" {
		opt.Unit = MgDL
	}
	w := opt.SparkWidth
	if w == 0 {
		w = sparkW
	}

	line := fmt.Sprintf("%s%sTIR %.0f%% | min %s | max %s | avg %s %s",
		sentinel, Prefix, sum.TIR, num(sum.Min, opt.Unit), num(sum.Max, opt.Unit), num(sum.Avg, opt.Unit), opt.Unit)
	if w > 0 {
		if sp := stats.Sparkline(samples, w); sp != "" {
			return line + "\n" + sp + endSentinel
		}
	}
	return line + endSentinel
}

// Value formats a glucose value given in mg/dL for display in unit u.
func Value(v float64, u Unit) string {
	if u == MmolL {
		return fmt.Sprintf("%.1f", v/stats.MmolFactor)
	}
	return fmt.Sprintf("%.0f", v)
}

func num(v float64, u Unit) string { return Value(v, u) }

// removeBlocks returns lines with every Glucava block removed, and how many
// were found. A block is a line starting with blockMarker (or
// legacyBlockMarker), bounded either by endSentinel — on that same line, or
// the line right after it — or, only when neither line carries endSentinel
// at all (a block written before it existed), by the old shape-based guess:
// the next line counts as part of the block if it looks like a sparkline.
// The lookahead never goes past the second line, matching the two shapes
// Block() actually produces (one line, or that line plus a sparkline), so a
// missing endSentinel can never make this eat unrelated text further down.
//
// A boundary that does not depend on the next blank line is deliberate:
// Strava's own editor has been observed collapsing blank lines between
// saves, and looping here means several duplicate blocks left over from that
// (already-written, before that fix) are all cleaned up the next time this
// activity is processed, not just the first one found.
func removeBlocks(lines []string) ([]string, int) {
	out := make([]string, 0, len(lines))
	found := 0
	for i := 0; i < len(lines); i++ {
		if !hasBlockPrefix(lines[i]) {
			out = append(out, lines[i])
			continue
		}
		found++
		switch {
		case strings.Contains(lines[i], endSentinel):
			// one line; already bounded.
		case i+1 < len(lines) && strings.Contains(lines[i+1], endSentinel):
			i++
		case i+1 < len(lines) && stats.LooksLikeSparkline(lines[i+1]):
			i++ // legacy block, no endSentinel anywhere: fall back to the old guess
		}
	}
	return out, found
}

// normalizeNewlines rewrites CRLF and lone CR line endings to LF. It must
// leave no "\r" behind: a stray, unpaired CR surviving a first pass (say,
// right before an LF that was not originally its pair) would look like a
// real CRLF pair to a second pass over the same text, and get eaten then —
// making Strip and Merge produce a different result the second time they see
// their own output. Handling both at once, in one pass, keeps the function
// idempotent regardless of how CR and LF are mixed in the input.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// Strip removes every Glucava block from existing and keeps the other text.
// It runs to a fixed point rather than a single pass: removing one block and
// trimming the result can put a second, still-marked line at the very start
// of some other line, where hasBlockPrefix would only recognise it once
// nothing precedes it — one pass could miss that and leave it as ordinary
// text, which a second call to Strip would then remove, so Strip's output
// would not equal Strip of its own output. Looping until a pass changes
// nothing rules that out regardless of how blocks and whitespace are mixed
// in the input; each pass only removes lines or trims, so it always
// terminates.
func Strip(existing string) string {
	s := strings.Trim(normalizeNewlines(existing), " \n\t")
	for {
		next := stripOnce(s)
		if next == s {
			return s
		}
		s = next
	}
}

func stripOnce(s string) string {
	lines, found := removeBlocks(strings.Split(s, "\n"))
	if found == 0 {
		return s
	}
	return strings.Trim(strings.Join(lines, "\n"), " \n\t")
}

// PreservesText reports whether merged differs from existing only by Glucava
// blocks: with every block removed, both texts must be identical. It is the
// invariant behind never touching the user's own description text, checked
// before anything is written to Strava.
func PreservesText(existing, merged string) bool {
	return Strip(existing) == Strip(merged)
}

// Merge puts block into existing. Any earlier Glucava block (see blockMarker)
// is removed first, then the new one is inserted where the first one was, or
// appended after a blank line if there wasn't one. Other text — including
// another app's own text that happens to share the 🩸 emoji — is kept as-is.
func Merge(existing, block string) string {
	existing = normalizeNewlines(existing)
	lines := strings.Split(existing, "\n")

	start := -1
	for i, l := range lines {
		if hasBlockPrefix(l) {
			start = i
			break
		}
	}
	if start >= 0 {
		rest, _ := removeBlocks(lines[start:])
		out := append([]string(nil), lines[:start]...)
		out = append(out, strings.Split(block, "\n")...)
		out = append(out, rest...)
		return strings.Join(out, "\n")
	}

	trimmed := strings.TrimRight(existing, " \n\t")
	if trimmed == "" {
		return block
	}
	return trimmed + "\n\n" + block
}
