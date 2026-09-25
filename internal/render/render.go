// Package render builds the Strava description block and merges it into existing text.
package render

import (
	"fmt"
	"strings"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// Prefix starts the first line of the block, shown to the reader.
const Prefix = "🩸 "

// blockMarker is what Merge and Strip actually match on to recognise a
// previous Glucava block. It is deliberately more specific than Prefix: the
// emoji alone collides with any other app that also starts a line with a
// blood drop (observed in the wild: another integration's own summary starts "🩸 Avg :
// ..."), which made Strip and Merge mistake someone else's text for ours and
// either eat or leave duplicates of it. TIR is the fixed word Block() always
// writes right after Prefix, so this string only ever matches our own line.
const blockMarker = Prefix + "TIR "

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

	line := fmt.Sprintf("%sTIR %.0f%% | min %s | max %s | avg %s %s",
		Prefix, sum.TIR, num(sum.Min, opt.Unit), num(sum.Max, opt.Unit), num(sum.Avg, opt.Unit), opt.Unit)
	if w > 0 {
		if sp := stats.Sparkline(samples, w); sp != "" {
			return line + "\n" + sp
		}
	}
	return line
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
// were found. A block is a line starting with blockMarker, plus the line
// right after it if that line is only sparkline characters (Block's own
// shape: one line, or that line plus a sparkline). Bounding it this way,
// rather than to the next blank line, is deliberate: Strava's own editor has
// been observed collapsing blank lines between saves, and looping here means
// several duplicate blocks left over from that (already-written, before this
// fix) are all cleaned up the next time this activity is processed, not just
// the first one found.
func removeBlocks(lines []string) ([]string, int) {
	out := make([]string, 0, len(lines))
	found := 0
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], blockMarker) {
			out = append(out, lines[i])
			continue
		}
		found++
		if i+1 < len(lines) && stats.LooksLikeSparkline(lines[i+1]) {
			i++
		}
	}
	return out, found
}

// Strip removes every Glucava block from existing and keeps the other text.
func Strip(existing string) string {
	existing = strings.ReplaceAll(existing, "\r\n", "\n")
	lines, found := removeBlocks(strings.Split(existing, "\n"))
	if found == 0 {
		return strings.TrimRight(existing, " \n\t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n \t")
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
	existing = strings.ReplaceAll(existing, "\r\n", "\n")
	lines := strings.Split(existing, "\n")

	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, blockMarker) {
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
