// Package importers parses one-shot CGM data exports (a file a person
// downloaded from their own account) into stats.Sample, in mg/dL. This is
// deliberately separate from glucose.Source: a Source is a live API glucava
// polls on its own; an importer is a single file, run once through the CLI,
// for backfilling history a live source never had (e.g. switching from
// another app, or restoring older readings a Source's retention already
// dropped).
//
// Adding a new format is one function satisfying Importer, registered by
// name in init(). See libre.go for the smallest example, or nightscout.go
// for one with a well-specified, stable format.
package importers

import (
	"bytes"
	"fmt"
	"io"
	"sort"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// Importer parses one export file into readings, in mg/dL, any order.
// Malformed individual rows should be counted and skipped, not fail the
// whole file: exports from real devices routinely include header banners,
// non-glucose rows (insulin, notes) and locale quirks.
type Importer interface {
	// Parse reads one export file. Skipped reports how many rows were
	// recognized as this format but could not be parsed (for the caller to
	// warn about); it does not include rows that are legitimately some other
	// record type (e.g. an insulin dose row in a Libre export).
	Parse(r io.Reader) (samples []stats.Sample, skipped int, err error)
}

var registry = map[string]Importer{}

// Register adds an importer under name. Call it from an init() func.
func Register(name string, imp Importer) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("importers: %q already registered", name))
	}
	registry[name] = imp
}

// Get returns the importer registered under name, if any.
func Get(name string) (Importer, bool) {
	imp, ok := registry[name]
	return imp, ok
}

// Names lists registered importer names, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ZipAware is implemented by an Importer that understands being handed a zip
// archive directly (see glookoImporter). CheckZipSupport uses it to give a
// clear error instead of a confusing low-level parse failure (e.g. a JSON or
// CSV parser choking on the zip's binary header) when a zip is handed to a
// format that expects a plain file.
type ZipAware interface {
	AcceptsZip() bool
}

// CheckZipSupport peeks the start of r, rewinding afterwards, and returns a
// clear error if it looks like a zip archive but imp does not accept one.
// Call it before imp.Parse(r).
func CheckZipSupport(imp Importer, r io.ReadSeeker, formatName string) error {
	magic := make([]byte, 4)
	n, _ := io.ReadFull(r, magic)
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("importers: rewind after peek: %w", err)
	}
	if n < 4 || !bytes.Equal(magic, zipMagic) {
		return nil
	}
	if za, ok := imp.(ZipAware); ok && za.AcceptsZip() {
		return nil
	}
	return fmt.Errorf("this file looks like a zip archive, but %q does not read zip files directly; extract it and point the import at the file inside, or pick a format that supports zip", formatName)
}
