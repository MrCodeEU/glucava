// Package render builds the Strava description block and merges it into existing text.
package render

import (
	"fmt"
	"strings"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// Prefix starts the first line of the block. Merge uses it to find a previous block.
const Prefix = "🩸 "

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

// Strip removes a Glucava block from existing and keeps the other text.
func Strip(existing string) string {
	existing = strings.ReplaceAll(existing, "\r\n", "\n")
	lines := strings.Split(existing, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, Prefix) {
			continue
		}
		end := i + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
			end++
		}
		out := append(append([]string(nil), lines[:i]...), lines[end:]...)
		return strings.Trim(strings.Join(out, "\n"), "\n \t")
	}
	return strings.TrimRight(existing, " \n\t")
}

// Merge puts block into existing. An earlier block, recognised by Prefix at the
// start of a line and running to the next blank line or the end, is replaced in
// place. Otherwise block is appended after a blank line. Other text is kept.
func Merge(existing, block string) string {
	existing = strings.ReplaceAll(existing, "\r\n", "\n")
	lines := strings.Split(existing, "\n")

	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, Prefix) {
			start = i
			break
		}
	}
	if start >= 0 {
		end := start + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
			end++
		}
		out := append([]string(nil), lines[:start]...)
		out = append(out, strings.Split(block, "\n")...)
		out = append(out, lines[end:]...)
		return strings.Join(out, "\n")
	}

	trimmed := strings.TrimRight(existing, " \n\t")
	if trimmed == "" {
		return block
	}
	return trimmed + "\n\n" + block
}
